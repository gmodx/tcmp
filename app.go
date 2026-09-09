package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	osc52 "github.com/aymanbagabas/go-osc52/v2"
	tea "github.com/charmbracelet/bubbletea"
	ansi "github.com/charmbracelet/x/ansi"
)

const (
	contentTop          = 3
	minimumWidth        = 60
	minimumHeight       = 14
	dividerWidth        = 3
	minimumPaneWidth    = 20
	cursorBlinkInterval = 500 * time.Millisecond
)

type cursorBlinkMsg time.Time

type appMode int

const (
	browseMode appMode = iota
	pasteMode
	menuMode
	helpMode
)

type editorState struct {
	side side
	row  int
	col  int
}

type pasteState struct {
	target side
	text   []rune
	cursor int
}

type textPoint struct {
	row int
	col int
}

type displayRow struct {
	rowIndex int
	part     int
}

type textSelection struct {
	side   side
	anchor textPoint
	head   textPoint
	active bool
}

type dragState struct {
	active bool
	side   side
	anchor textPoint
}

type contextMenuState struct {
	x     int
	y     int
	kind  contextMenuKind
	point textPoint
}

type contextMenuKind int

const (
	lineContextMenu contextMenuKind = iota
	textContextMenu
	fileMenu
	editMenu
	compareMenu
	helpMenu
)

func (kind contextMenuKind) isTopMenu() bool {
	return kind >= fileMenu && kind <= helpMenu
}

type clipboardWriteMsg struct {
	err error
}

type appModel struct {
	workspace         workspace
	focus             side
	current           int
	scroll            int
	horizontal        [2]int
	wrap              bool
	width             int
	height            int
	mode              appMode
	editor            editorState
	caretActive       bool
	cursorVisible     bool
	paste             pasteState
	drag              dragState
	textSelection     textSelection
	menu              contextMenuState
	menuItem          int
	clipboard         string
	clipboardOut      io.Writer
	splitRatio        float64
	dividerDragging   bool
	dividerDragOffset int
	dividerHover      bool
	status            string
}

func newAppModel(left, right []string) appModel {
	return appModel{
		workspace:     newWorkspace(left, right),
		editor:        editorState{side: leftSide},
		caretActive:   true,
		cursorVisible: true,
		clipboardOut:  os.Stderr,
		wrap:          true,
		splitRatio:    0.5,
		status:        "Ready. Click to edit, drag to select, or use the menu and command bars.",
	}
}

func blinkCursor() tea.Cmd {
	return tea.Tick(cursorBlinkInterval, func(now time.Time) tea.Msg {
		return cursorBlinkMsg(now)
	})
}

func (m appModel) Init() tea.Cmd { return blinkCursor() }

func (m appModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case cursorBlinkMsg:
		m.cursorVisible = !m.cursorVisible
		return m, blinkCursor()
	case clipboardWriteMsg:
		if message.err != nil {
			m.status = "Copied inside tcomp, but the terminal clipboard write failed: " + message.err.Error()
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = message.Width, message.Height
		m.clampViewport()
		return m, nil
	case tea.MouseMsg:
		if message.Action == tea.MouseActionPress {
			m.cursorVisible = true
		}
		return m.handleMouse(message)
	case tea.KeyMsg:
		m.cursorVisible = true
		m.dividerHover = false
		m.dividerDragging = false
		if message.Type == tea.KeyCtrlQ || message.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		switch m.mode {
		case pasteMode:
			return m.handlePasteKey(message)
		case menuMode:
			return m.handleMenuKey(message)
		case helpMode:
			return m.handleHelpKey(message)
		default:
			return m.handleBrowseKey(message)
		}
	}
	return m, nil
}

func (m appModel) handleBrowseKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Paste {
		m.receiveExternalPaste(string(key.Runes))
		return m, nil
	}
	if key.Type == tea.KeyRunes && !key.Alt && (len(key.Runes) > 1 || strings.ContainsRune(string(key.Runes), '\n')) {
		m.receiveExternalPaste(string(key.Runes))
		return m, nil
	}

	switch key.String() {
	case "ctrl+a":
		m.selectAllText()
	case "ctrl+x":
		return m.cutTextSelection()
	case "ctrl+v":
		m.pasteInternalClipboard()
	case "ctrl+p":
		m.beginPaste("")
	case "ctrl+r":
		m.resetComparisonState()
	case "alt+f":
		m.openTopMenu(fileMenu)
	case "alt+e":
		m.openTopMenu(editMenu)
	case "alt+c":
		m.openTopMenu(compareMenu)
	case "alt+m":
		m.applyManualMatch()
	case "alt+h":
		m.openHelp()
	case "esc":
		m.workspace.clearSelections()
		m.textSelection = textSelection{}
		m.status = "Selections cleared."
	case "tab", "shift+tab":
		m.focus = 1 - m.focus
		m.caretActive = false
		m.syncCaretToCurrent()
		m.status = sideName(m.focus) + " pane focused."
	case "up":
		m.ensureEditableCaret()
		m.moveEditorRow(-1)
	case "down":
		m.ensureEditableCaret()
		m.moveEditorRow(1)
	case "pgup", "pageup":
		m.ensureEditableCaret()
		m.moveEditorRow(-m.bodyRows())
	case "pgdown", "pagedown":
		m.ensureEditableCaret()
		m.moveEditorRow(m.bodyRows())
	case "left":
		m.ensureEditableCaret()
		m.editor.col = max(0, m.editor.col-1)
	case "right":
		m.ensureEditableCaret()
		m.editor.col = min(m.editor.col+1, utf8.RuneCountInString(m.editorLine()))
	case "home":
		m.ensureEditableCaret()
		m.editor.col = 0
	case "end", "ctrl+e":
		m.ensureEditableCaret()
		m.editor.col = utf8.RuneCountInString(m.editorLine())
	case "ctrl+home":
		m.ensureEditableCaret()
		m.moveEditorToBoundary(false)
	case "ctrl+end":
		m.ensureEditableCaret()
		m.moveEditorToBoundary(true)
	case "enter":
		m.replaceSelectionOrInsert("\n")
	case "backspace":
		if m.textSelection.active {
			m.replaceTextSelection("")
		} else {
			m.ensureEditableCaret()
			m.backspaceEditor()
		}
	case "delete":
		if m.textSelection.active {
			m.replaceTextSelection("")
		} else {
			m.ensureEditableCaret()
			m.deleteEditor()
		}
	case "ctrl+s":
		m.status = "Changes are applied immediately."
	default:
		if key.Type == tea.KeyRunes && !key.Alt {
			m.replaceSelectionOrInsert(string(key.Runes))
		}
	}
	m.keepEditorVisible()
	return m, nil
}

func (m appModel) handlePasteKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+s":
		m.workspace.replaceDocument(m.paste.target, splitText(string(m.paste.text)))
		m.focus = m.paste.target
		m.current, m.scroll, m.horizontal = 0, 0, [2]int{}
		m.mode = browseMode
		m.status = sideName(m.focus) + " text replaced; differences recomputed."
		return m, nil
	case "esc":
		m.mode = browseMode
		m.status = "Paste cancelled."
		return m, nil
	case "left":
		m.paste.cursor = max(0, m.paste.cursor-1)
		return m, nil
	case "right":
		m.paste.cursor = min(len(m.paste.text), m.paste.cursor+1)
		return m, nil
	case "pgup", "pageup":
		m.movePasteCursorRows(-m.bodyRows())
		return m, nil
	case "pgdown", "pagedown":
		m.movePasteCursorRows(m.bodyRows())
		return m, nil
	case "home", "ctrl+a":
		m.paste.cursor = pasteLineStart(m.paste.text, m.paste.cursor)
		return m, nil
	case "end", "ctrl+e":
		m.paste.cursor = pasteLineEnd(m.paste.text, m.paste.cursor)
		return m, nil
	case "ctrl+home":
		m.paste.cursor = 0
		return m, nil
	case "ctrl+end":
		m.paste.cursor = len(m.paste.text)
		return m, nil
	case "ctrl+v":
		if m.clipboard == "" {
			m.status = "The tcomp clipboard is empty; use the terminal paste shortcut for external text."
		} else {
			m.insertPasteText(m.clipboard)
			m.status = "Clipboard text inserted into the replacement preview."
		}
		return m, nil
	case "backspace":
		if m.paste.cursor > 0 {
			m.paste.text = append(m.paste.text[:m.paste.cursor-1], m.paste.text[m.paste.cursor:]...)
			m.paste.cursor--
		}
		return m, nil
	case "delete":
		if m.paste.cursor < len(m.paste.text) {
			m.paste.text = append(m.paste.text[:m.paste.cursor], m.paste.text[m.paste.cursor+1:]...)
		}
		return m, nil
	case "enter":
		m.insertPasteText("\n")
		return m, nil
	case "tab":
		m.insertPasteText("\t")
		return m, nil
	}
	if key.Paste || (key.Type == tea.KeyRunes && !key.Alt) {
		m.insertPasteText(string(key.Runes))
	}
	return m, nil
}

func (m appModel) handleMenuKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	itemCount := len(m.contextMenuItems())
	switch key.String() {
	case "left":
		if m.menu.kind.isTopMenu() {
			m.moveTopMenu(-1)
		}
	case "right":
		if m.menu.kind.isTopMenu() {
			m.moveTopMenu(1)
		}
	case "up", "k":
		m.menuItem = (m.menuItem + itemCount - 1) % itemCount
	case "down", "j", "tab":
		m.menuItem = (m.menuItem + 1) % itemCount
	case "enter", " ":
		return m.activateMenuItem()
	case "ctrl+x":
		if m.menu.kind == textContextMenu {
			m.mode = browseMode
			return m.cutTextSelection()
		}
	case "ctrl+v":
		if m.menu.kind == textContextMenu {
			m.mode = browseMode
			m.pasteInternalClipboard()
		}
	case "m":
		if m.menu.kind == lineContextMenu {
			m.mode = browseMode
			m.applyManualMatch()
		}
	case "r":
		if m.menu.kind == lineContextMenu {
			m.mode = browseMode
			m.resetComparisonState()
		}
	case "esc", "q":
		m.mode = browseMode
		m.status = "Context menu closed."
	}
	m.clampViewport()
	return m, nil
}

func (m appModel) handleHelpKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc", "q", "?", "alt+h", "enter":
		m.mode = browseMode
		m.status = "Help closed."
	}
	return m, nil
}

func (m *appModel) openHelp() {
	m.drag.active = false
	m.mode = helpMode
	m.status = "Help is open; press Esc or click Close to return."
}

func (m *appModel) resetComparisonState() {
	m.workspace.resetMatches()
	m.workspace.clearSelections()
	m.textSelection = textSelection{}
	m.drag.active = false
	m.status = "Manual matches and selections were reset."
}

func (m appModel) handleMouse(mouse tea.MouseMsg) (tea.Model, tea.Cmd) {
	overDivider := m.dividerCell(mouse.X, mouse.Y)
	switch mouse.Action {
	case tea.MouseActionMotion:
		m.dividerHover = overDivider
	case tea.MouseActionPress:
		if !overDivider {
			m.dividerHover = false
			m.dividerDragging = false
		}
	}

	if m.mode != pasteMode && mouse.Y == 0 {
		if kind, ok := m.topMenuAt(mouse.X); ok {
			if mouse.Action == tea.MouseActionMotion && m.mode == menuMode && m.menu.kind.isTopMenu() {
				m.openTopMenu(kind)
				return m, nil
			}
			if mouse.Action == tea.MouseActionPress && mouse.Button == tea.MouseButtonLeft {
				if m.mode == menuMode && m.menu.kind == kind {
					m.mode = browseMode
					m.status = "Menu closed."
				} else {
					m.openTopMenu(kind)
				}
				return m, nil
			}
		}
	}
	if m.mode != pasteMode && mouse.Action == tea.MouseActionPress && mouse.Button == tea.MouseButtonLeft && mouse.Y == m.height-1 {
		if action, ok := m.commandBarActionAt(mouse.X); ok {
			return m.activateCommandBar(action)
		}
	}
	if m.mode == helpMode {
		if mouse.Action == tea.MouseActionPress && mouse.Button == tea.MouseButtonLeft && m.helpCloseAt(mouse.X, mouse.Y) {
			m.mode = browseMode
			m.status = "Help closed."
		}
		return m, nil
	}

	if mouse.Shift && (mouse.Button == tea.MouseButtonWheelUp || mouse.Button == tea.MouseButtonWheelDown) {
		which, ok := m.sideAtX(mouse.X)
		if !ok {
			which = m.focus
		}
		delta := -4
		if mouse.Button == tea.MouseButtonWheelDown {
			delta = 4
		}
		m.scrollHorizontal(which, delta)
		return m, nil
	}
	switch mouse.Button {
	case tea.MouseButtonWheelUp:
		m.mode = modeWithoutMenu(m.mode)
		m.scrollBy(-3)
		return m, nil
	case tea.MouseButtonWheelDown:
		m.mode = modeWithoutMenu(m.mode)
		m.scrollBy(3)
		return m, nil
	case tea.MouseButtonWheelLeft, tea.MouseButtonWheelRight:
		which, ok := m.sideAtX(mouse.X)
		if !ok {
			which = m.focus
		}
		delta := -4
		if mouse.Button == tea.MouseButtonWheelRight {
			delta = 4
		}
		m.scrollHorizontal(which, delta)
		return m, nil
	}
	if m.mode == pasteMode {
		return m, nil
	}
	if m.mode == menuMode {
		if mouse.Action == tea.MouseActionRelease {
			return m, nil
		}
		if item, ok := m.menuItemAt(mouse.X, mouse.Y); ok {
			m.menuItem = item
			if mouse.Action == tea.MouseActionPress && mouse.Button == tea.MouseButtonLeft {
				return m.activateMenuItem()
			}
			return m, nil
		}
		if mouse.Action == tea.MouseActionMotion {
			return m, nil
		}
		m.mode = browseMode
	}

	if m.dividerDragging {
		switch mouse.Action {
		case tea.MouseActionMotion:
			m.setDividerX(mouse.X - m.dividerDragOffset)
			m.clampViewport()
			return m, nil
		case tea.MouseActionRelease:
			m.setDividerX(mouse.X - m.dividerDragOffset)
			m.dividerDragging = false
			m.dividerHover = m.dividerCell(mouse.X, mouse.Y)
			m.status = fmt.Sprintf("Pane widths adjusted to %d / %d.", m.paneWidth(leftSide), m.paneWidth(rightSide))
			m.clampViewport()
			return m, nil
		}
	}
	if mouse.Action == tea.MouseActionPress && mouse.Button == tea.MouseButtonLeft && m.dividerCell(mouse.X, mouse.Y) {
		m.drag.active = false
		m.dividerDragging = true
		m.dividerHover = true
		m.dividerDragOffset = mouse.X - m.dividerX()
		m.status = "Drag the center divider left or right to resize the panes."
		return m, nil
	}

	if mouse.Action == tea.MouseActionRelease {
		if m.drag.active {
			m.extendTextDrag(mouse.X, mouse.Y)
		}
		m.drag.active = false
		return m, nil
	}
	if mouse.Action == tea.MouseActionMotion && m.drag.active {
		m.extendTextDrag(mouse.X, mouse.Y)
		return m, nil
	}
	if mouse.Action != tea.MouseActionPress {
		return m, nil
	}

	clickedSide, ok := m.sideAtX(mouse.X)
	if !ok || mouse.Y < contentTop || mouse.Y >= contentTop+m.bodyRows() {
		return m, nil
	}
	visualIndex := m.scroll + mouse.Y - contentTop
	visibleRows := m.displayRows()
	if visualIndex < 0 || visualIndex >= len(visibleRows) {
		return m, nil
	}
	rowIndex := visibleRows[visualIndex].rowIndex
	m.focus, m.current = clickedSide, rowIndex
	switch mouse.Button {
	case tea.MouseButtonLeft:
		point, ok := m.textPointAt(mouse.X, mouse.Y)
		if !ok {
			return m, nil
		}
		sourceIndex := m.sourceIndexAt(clickedSide, point.row)
		if sourceIndex < 0 {
			return m, nil
		}
		m.editor = editorState{side: clickedSide, row: sourceIndex, col: point.col}
		m.caretActive = true
		m.textSelection = textSelection{side: clickedSide, anchor: point, head: point}
		m.drag = dragState{active: true, side: clickedSide, anchor: point}
		m.status = "Caret placed; drag across characters to select text."
	case tea.MouseButtonRight:
		point, pointOK := m.textPointAt(mouse.X, mouse.Y)
		if pointOK && m.textSelectionContains(clickedSide, point) {
			m.openContextMenu(mouse.X, mouse.Y, textContextMenu, point)
			m.status = "Choose Copy, Cut, or Paste for the selected text."
			return m, nil
		}
		sourceIndex := m.sourceIndexAt(clickedSide, rowIndex)
		if sourceIndex < 0 {
			return m, nil
		}
		if !m.workspace.isSelected(clickedSide, sourceIndex) {
			m.workspace.selectLine(clickedSide, sourceIndex)
		}
		m.openContextMenu(mouse.X, mouse.Y, lineContextMenu, point)
	}
	m.clampViewport()
	return m, nil
}

func modeWithoutMenu(mode appMode) appMode {
	if mode == menuMode {
		return browseMode
	}
	return mode
}

func (m *appModel) extendTextDrag(x, y int) {
	paneStart := m.paneStart(m.drag.side)
	paneWidth := m.paneWidth(m.drag.side)
	contentStart := paneStart + m.gutterWidth()
	contentEnd := paneStart + paneWidth - 1
	if x <= contentStart {
		m.scrollHorizontal(m.drag.side, -1)
	} else if x >= contentEnd {
		m.scrollHorizontal(m.drag.side, 1)
	}
	x = max(contentStart, min(contentEnd, x))
	if y < contentTop {
		m.scrollBy(-1)
		y = contentTop
	} else if y >= contentTop+m.bodyRows() {
		m.scrollBy(1)
		y = contentTop + m.bodyRows() - 1
	}
	point, ok := m.textPointAt(x, y)
	if !ok {
		return
	}
	m.focus, m.current = m.drag.side, point.row
	if sourceIndex := m.sourceIndexAt(m.drag.side, point.row); sourceIndex >= 0 {
		m.editor = editorState{side: m.drag.side, row: sourceIndex, col: point.col}
		m.caretActive = true
	}
	m.textSelection = textSelection{
		side:   m.drag.side,
		anchor: m.drag.anchor,
		head:   point,
		active: point != m.drag.anchor,
	}
	if m.textSelection.active {
		m.status = fmt.Sprintf("%d characters selected in the %s pane.", utf8.RuneCountInString(m.selectedText()), strings.ToLower(sideName(m.drag.side)))
	}
	m.clampViewport()
}

func (m appModel) displayRows() []displayRow {
	rows := make([]displayRow, 0, len(m.workspace.rows))
	for rowIndex, row := range m.workspace.rows {
		height := 1
		if m.wrap {
			height = max(m.wrappedRowHeight(row, leftSide), m.wrappedRowHeight(row, rightSide))
		}
		for part := 0; part < height; part++ {
			rows = append(rows, displayRow{rowIndex: rowIndex, part: part})
		}
	}
	return rows
}

func (m appModel) wrappedRowHeight(row Row, which side) int {
	lineNumber := row.LeftNum
	if which == rightSide {
		lineNumber = row.RightNum
	}
	if lineNumber == 0 {
		return 1
	}
	width := ansi.StringWidth(displayText(rowText(row, which)))
	return max(1, (width+m.contentWidth(which)-1)/m.contentWidth(which))
}

func (m appModel) contentWidth(which side) int {
	return max(1, m.paneWidth(which)-m.gutterWidth())
}

func (m appModel) visualRowIndex(rowIndex, part int) int {
	for index, row := range m.displayRows() {
		if row.rowIndex == rowIndex && row.part == part {
			return index
		}
	}
	return 0
}

func (m appModel) textPointAt(x, y int) (textPoint, bool) {
	which, ok := m.sideAtX(x)
	if !ok || y < contentTop || y >= contentTop+m.bodyRows() {
		return textPoint{}, false
	}
	visualIndex := m.scroll + y - contentTop
	visibleRows := m.displayRows()
	if visualIndex < 0 || visualIndex >= len(visibleRows) {
		return textPoint{}, false
	}
	displayRow := visibleRows[visualIndex]
	rowIndex := displayRow.rowIndex
	row := m.workspace.rows[rowIndex]
	text := row.Left
	lineNumber := row.LeftNum
	paneStart := m.paneStart(which)
	if which == rightSide {
		text = row.Right
		lineNumber = row.RightNum
	}
	if lineNumber == 0 {
		return textPoint{}, false
	}
	contentColumn := max(0, x-paneStart-m.gutterWidth())
	if m.horizontal[which] > 0 && contentColumn > 0 {
		contentColumn--
	}
	cell := m.horizontal[which] + contentColumn
	if m.wrap {
		cell = displayRow.part*m.contentWidth(which) + contentColumn
	}
	return textPoint{row: rowIndex, col: runeIndexAtDisplayColumn(text, cell)}, true
}

func runeIndexAtDisplayColumn(text string, column int) int {
	if column <= 0 {
		return 0
	}
	width := 0
	for index, character := range []rune(text) {
		next := width + ansi.StringWidth(displayText(string(character)))
		if column < next {
			return index
		}
		width = next
	}
	return utf8.RuneCountInString(text)
}

func (m *appModel) openContextMenu(x, y int, kind contextMenuKind, point textPoint) {
	m.drag.active = false
	m.mode, m.menuItem = menuMode, 0
	m.menu = contextMenuState{x: x, y: y, kind: kind, point: point}
	m.status = "Choose a context action."
}

func (m *appModel) openTopMenu(kind contextMenuKind) {
	m.drag.active = false
	m.mode, m.menuItem = menuMode, 0
	m.menu = contextMenuState{x: m.topMenuX(kind), y: 1, kind: kind}
	m.status = "Choose a menu command or press Esc to close the menu."
}

func (m *appModel) moveTopMenu(delta int) {
	for index, item := range topMenuItems {
		if item.kind != m.menu.kind {
			continue
		}
		next := (index + delta + len(topMenuItems)) % len(topMenuItems)
		m.openTopMenu(topMenuItems[next].kind)
		return
	}
}

func (m appModel) sourceIndexAt(which side, rowIndex int) int {
	if rowIndex < 0 || rowIndex >= len(m.workspace.rows) {
		return -1
	}
	number := m.workspace.rows[rowIndex].LeftNum
	if which == rightSide {
		number = m.workspace.rows[rowIndex].RightNum
	}
	return number - 1
}

func (m appModel) orderedTextSelection() (textPoint, textPoint, bool) {
	selection := m.textSelection
	if !selection.active {
		return textPoint{}, textPoint{}, false
	}
	start, end := selection.anchor, selection.head
	if start.row > end.row || (start.row == end.row && start.col > end.col) {
		start, end = end, start
	}
	return start, end, start != end
}

func (m appModel) textSelectionRange(which side, rowIndex, length int) (int, int, bool) {
	if m.textSelection.side != which {
		return 0, 0, false
	}
	start, end, ok := m.orderedTextSelection()
	if !ok || rowIndex < start.row || rowIndex > end.row {
		return 0, 0, false
	}
	from, to := 0, length
	if rowIndex == start.row {
		from = start.col
	}
	if rowIndex == end.row {
		to = end.col
	}
	from = max(0, min(length, from))
	to = max(from, min(length, to))
	return from, to, from < to
}

func (m appModel) textSelectionContains(which side, point textPoint) bool {
	if m.textSelection.side != which {
		return false
	}
	start, end, ok := m.orderedTextSelection()
	if !ok || point.row < start.row || point.row > end.row {
		return false
	}
	if point.row == start.row && point.col < start.col {
		return false
	}
	if point.row == end.row && point.col >= end.col {
		return false
	}
	return true
}

func (m appModel) selectedText() string {
	start, end, ok := m.orderedTextSelection()
	if !ok {
		return ""
	}
	startSource := m.sourceIndexAt(m.textSelection.side, start.row)
	endSource := m.sourceIndexAt(m.textSelection.side, end.row)
	if startSource < 0 || endSource < startSource {
		return ""
	}
	document := m.workspace.lines(m.textSelection.side)
	lines := make([]string, 0, endSource-startSource+1)
	for sourceIndex := startSource; sourceIndex <= endSource; sourceIndex++ {
		text := document[sourceIndex]
		runes := []rune(text)
		from, to := 0, len(runes)
		if sourceIndex == startSource {
			from = max(0, min(len(runes), start.col))
		}
		if sourceIndex == endSource {
			to = max(0, min(len(runes), end.col))
		}
		if to < from {
			to = from
		}
		lines = append(lines, string(runes[from:to]))
	}
	return strings.Join(lines, "\n")
}

func (m *appModel) selectAllText() {
	firstRow, lastRow := -1, -1
	for rowIndex := range m.workspace.rows {
		if m.sourceIndexAt(m.focus, rowIndex) >= 0 {
			if firstRow < 0 {
				firstRow = rowIndex
			}
			lastRow = rowIndex
		}
	}
	if firstRow < 0 {
		m.status = "The focused pane has no text to select."
		return
	}
	lastText := m.workspace.lines(m.focus)[m.sourceIndexAt(m.focus, lastRow)]
	m.textSelection = textSelection{
		side:   m.focus,
		anchor: textPoint{row: firstRow, col: 0},
		head:   textPoint{row: lastRow, col: utf8.RuneCountInString(lastText)},
		active: firstRow != lastRow || lastText != "",
	}
	m.status = fmt.Sprintf("%d characters selected in the %s pane.", utf8.RuneCountInString(m.selectedText()), strings.ToLower(sideName(m.focus)))
}

func (m appModel) copyTextSelection() (tea.Model, tea.Cmd) {
	text := m.selectedText()
	if text == "" {
		m.status = "Select text before copying."
		return m, nil
	}
	m.clipboard = text
	m.status = fmt.Sprintf("Copied %d characters.", utf8.RuneCountInString(text))
	return m, writeTerminalClipboard(m.clipboardOut, text)
}

func (m appModel) cutTextSelection() (tea.Model, tea.Cmd) {
	text := m.selectedText()
	if text == "" {
		m.status = "Select text before cutting."
		return m, nil
	}
	m.clipboard = text
	if !m.replaceTextSelection("") {
		m.status = "The selected text could not be cut."
		return m, nil
	}
	m.status = fmt.Sprintf("Cut %d characters.", utf8.RuneCountInString(text))
	return m, writeTerminalClipboard(m.clipboardOut, text)
}

func writeTerminalClipboard(output io.Writer, text string) tea.Cmd {
	return func() tea.Msg {
		_, err := io.WriteString(output, osc52.New(text).String())
		return clipboardWriteMsg{err: err}
	}
}

func (m *appModel) pasteInternalClipboard() {
	if m.clipboard == "" {
		m.status = "The tcomp clipboard is empty; use Shift+Insert or Ctrl+Shift+V for external text."
		return
	}
	if m.textSelection.active {
		if m.replaceTextSelection(m.clipboard) {
			m.status = fmt.Sprintf("Pasted %d characters over the selection.", utf8.RuneCountInString(m.clipboard))
		}
		return
	}
	m.insertAtCaret(m.clipboard)
	m.status = fmt.Sprintf("Pasted %d characters at the caret.", utf8.RuneCountInString(m.clipboard))
}

func (m *appModel) receiveExternalPaste(text string) {
	text = normalizeNewlines(text)
	if m.textSelection.active {
		if m.replaceTextSelection(text) {
			m.status = fmt.Sprintf("Pasted %d characters over the selection.", utf8.RuneCountInString(text))
		}
		return
	}
	m.insertAtCaret(text)
	m.status = fmt.Sprintf("Pasted %d characters at the caret.", utf8.RuneCountInString(text))
}

func (m *appModel) replaceSelectionOrInsert(text string) {
	if m.textSelection.active {
		m.replaceTextSelection(text)
		return
	}
	m.insertAtCaret(text)
}

func (m *appModel) insertAtCaret(text string) {
	m.ensureEditableCaret()
	m.insertEditorText(text)
	m.mode = browseMode
	m.caretActive = true
	m.textSelection = textSelection{}
	m.keepEditorVisible()
}

func (m appModel) validCaret() bool {
	if !m.caretActive || m.editor.side != m.focus {
		return false
	}
	lines := m.workspace.lines(m.editor.side)
	if m.editor.row < 0 || m.editor.row >= len(lines) {
		return false
	}
	return m.editor.col >= 0 && m.editor.col <= utf8.RuneCountInString(lines[m.editor.row])
}

func (m *appModel) replaceTextSelection(replacement string) bool {
	start, end, ok := m.orderedTextSelection()
	if !ok {
		return false
	}
	which := m.textSelection.side
	startSource := m.sourceIndexAt(which, start.row)
	endSource := m.sourceIndexAt(which, end.row)
	if startSource < 0 || endSource < startSource {
		return false
	}
	document := append([]string(nil), m.workspace.lines(which)...)
	startRunes := []rune(document[startSource])
	endRunes := []rune(document[endSource])
	start.col = max(0, min(len(startRunes), start.col))
	end.col = max(0, min(len(endRunes), end.col))
	prefix := string(startRunes[:start.col])
	suffix := string(endRunes[end.col:])
	parts := strings.Split(normalizeNewlines(replacement), "\n")
	replacementLines := append([]string(nil), parts...)
	replacementLines[0] = prefix + replacementLines[0]
	replacementLines[len(replacementLines)-1] += suffix
	caretColumn := utf8.RuneCountInString(parts[len(parts)-1])
	if len(parts) == 1 {
		caretColumn += utf8.RuneCountInString(prefix)
	}

	updated := make([]string, 0, len(document)-(endSource-startSource+1)+len(replacementLines))
	updated = append(updated, document[:startSource]...)
	updated = append(updated, replacementLines...)
	updated = append(updated, document[endSource+1:]...)
	m.workspace.updateDocument(which, updated, lineChange{
		Start: startSource, Deleted: endSource - startSource + 1, Inserted: len(replacementLines),
	})
	m.focus = which
	m.editor = editorState{side: which, row: startSource + len(replacementLines) - 1, col: caretColumn}
	m.caretActive = true
	m.current = m.rowForSource(which, m.editor.row)
	m.textSelection = textSelection{}
	m.drag.active = false
	m.clampViewport()
	return true
}

func (m *appModel) ensureEditableCaret() {
	m.textSelection = textSelection{}
	if m.validCaret() {
		m.current = m.rowForSource(m.editor.side, m.editor.row)
		return
	}

	lineIndex := m.sourceIndexAt(m.focus, m.current)
	if lineIndex < 0 {
		lineIndex = max(0, min(len(m.workspace.lines(m.focus))-1, m.insertionIndex(m.focus, m.current)))
	}
	column := 0
	if m.caretActive {
		column = m.editor.col
	}
	lineLength := utf8.RuneCountInString(m.workspace.lines(m.focus)[lineIndex])
	m.editor = editorState{side: m.focus, row: lineIndex, col: max(0, min(lineLength, column))}
	m.caretActive = true
	m.current = m.rowForSource(m.focus, lineIndex)
}

func (m *appModel) beginPaste(initial string) {
	m.textSelection = textSelection{}
	initial = normalizeNewlines(initial)
	m.mode = pasteMode
	m.paste = pasteState{target: m.focus, text: []rune(initial), cursor: utf8.RuneCountInString(initial)}
	m.status = "Paste or type text; Ctrl+S replaces the focused document and Esc cancels."
}

func (m *appModel) insertPasteText(text string) {
	runes := []rune(normalizeNewlines(text))
	updated := make([]rune, 0, len(m.paste.text)+len(runes))
	updated = append(updated, m.paste.text[:m.paste.cursor]...)
	updated = append(updated, runes...)
	updated = append(updated, m.paste.text[m.paste.cursor:]...)
	m.paste.text = updated
	m.paste.cursor += len(runes)
}

func (m *appModel) insertEditorText(text string) {
	lines := append([]string(nil), m.workspace.lines(m.editor.side)...)
	line := lines[m.editor.row]
	left, right := splitRunes(line, m.editor.col)
	parts := strings.Split(normalizeNewlines(text), "\n")
	if len(parts) == 1 {
		lines[m.editor.row] = left + parts[0] + right
		m.editor.col += utf8.RuneCountInString(parts[0])
		m.workspace.updateDocument(m.editor.side, lines, lineChange{Start: m.editor.row, Deleted: 1, Inserted: 1})
	} else {
		replacement := make([]string, 0, len(parts))
		replacement = append(replacement, left+parts[0])
		replacement = append(replacement, parts[1:len(parts)-1]...)
		replacement = append(replacement, parts[len(parts)-1]+right)
		updated := make([]string, 0, len(lines)+len(replacement)-1)
		updated = append(updated, lines[:m.editor.row]...)
		updated = append(updated, replacement...)
		updated = append(updated, lines[m.editor.row+1:]...)
		oldRow := m.editor.row
		m.editor.row += len(replacement) - 1
		m.editor.col = utf8.RuneCountInString(parts[len(parts)-1])
		m.workspace.updateDocument(m.editor.side, updated, lineChange{Start: oldRow, Deleted: 1, Inserted: len(replacement)})
	}
	m.current = m.rowForSource(m.editor.side, m.editor.row)
}

func (m *appModel) backspaceEditor() {
	lines := append([]string(nil), m.workspace.lines(m.editor.side)...)
	if m.editor.col > 0 {
		left, right := splitRunes(lines[m.editor.row], m.editor.col)
		leftRunes := []rune(left)
		lines[m.editor.row] = string(leftRunes[:len(leftRunes)-1]) + right
		m.editor.col--
		m.workspace.updateDocument(m.editor.side, lines, lineChange{Start: m.editor.row, Deleted: 1, Inserted: 1})
	} else if m.editor.row > 0 {
		start := m.editor.row - 1
		m.editor.col = utf8.RuneCountInString(lines[start])
		lines[start] += lines[m.editor.row]
		lines = append(lines[:m.editor.row], lines[m.editor.row+1:]...)
		m.editor.row--
		m.workspace.updateDocument(m.editor.side, lines, lineChange{Start: start, Deleted: 2, Inserted: 1})
	}
	m.current = m.rowForSource(m.editor.side, m.editor.row)
}

func (m *appModel) deleteEditor() {
	lines := append([]string(nil), m.workspace.lines(m.editor.side)...)
	line := lines[m.editor.row]
	if m.editor.col < utf8.RuneCountInString(line) {
		left, right := splitRunes(line, m.editor.col)
		rightRunes := []rune(right)
		lines[m.editor.row] = left + string(rightRunes[1:])
		m.workspace.updateDocument(m.editor.side, lines, lineChange{Start: m.editor.row, Deleted: 1, Inserted: 1})
	} else if m.editor.row < len(lines)-1 {
		lines[m.editor.row] += lines[m.editor.row+1]
		lines = append(lines[:m.editor.row+1], lines[m.editor.row+2:]...)
		m.workspace.updateDocument(m.editor.side, lines, lineChange{Start: m.editor.row, Deleted: 2, Inserted: 1})
	}
	m.current = m.rowForSource(m.editor.side, m.editor.row)
}

func (m *appModel) moveEditorRow(delta int) {
	m.editor.row = max(0, min(len(m.workspace.lines(m.editor.side))-1, m.editor.row+delta))
	m.editor.col = min(m.editor.col, utf8.RuneCountInString(m.editorLine()))
	m.current = m.rowForSource(m.editor.side, m.editor.row)
}

func (m *appModel) applyManualMatch() {
	left := m.workspace.selected[leftSide]
	right := m.workspace.selected[rightSide]
	if err := m.workspace.addSelectedMatches(); err != nil {
		m.status = "Cannot match lines: " + err.Error() + "."
		return
	}
	if left.count() == 1 {
		m.status = fmt.Sprintf("Manual match created: Left %d ↔ Right %d.", left.Start+1, right.Start+1)
	} else {
		m.status = fmt.Sprintf("%d manual matches created: Left %d–%d ↔ Right %d–%d.", left.count(), left.Start+1, left.End+1, right.Start+1, right.End+1)
	}
	m.current = m.rowForSource(m.focus, m.workspace.picked[m.focus])
}

func (m *appModel) moveEditorToBoundary(toEnd bool) {
	lines := m.workspace.lines(m.editor.side)
	if toEnd {
		m.editor.row = len(lines) - 1
		m.editor.col = utf8.RuneCountInString(lines[m.editor.row])
	} else {
		m.editor.row = 0
		m.editor.col = 0
	}
	m.current = m.rowForSource(m.editor.side, m.editor.row)
}

func (m appModel) activateCommandBar(action commandBarAction) (tea.Model, tea.Cmd) {
	switch action {
	case commandHelp:
		if m.mode == helpMode {
			m.mode = browseMode
			m.status = "Help closed."
		} else {
			m.openHelp()
		}
	case commandReset:
		m.mode = browseMode
		m.resetComparisonState()
	case commandReplace:
		m.mode = browseMode
		m.beginPaste("")
	case commandMatch:
		m.mode = browseMode
		m.applyManualMatch()
	case commandCopy:
		m.mode = browseMode
		return m.copyTextSelection()
	case commandQuit:
		return m, tea.Quit
	}
	return m, nil
}

func (m appModel) activateMenuItem() (tea.Model, tea.Cmd) {
	switch m.menu.kind {
	case textContextMenu:
		switch m.menuItem {
		case 0:
			m.mode = browseMode
			return m.copyTextSelection()
		case 1:
			m.mode = browseMode
			return m.cutTextSelection()
		case 2:
			m.mode = browseMode
			m.pasteInternalClipboard()
		default:
			m.mode = browseMode
			m.status = "Context menu closed."
		}
	case lineContextMenu:
		switch m.menuItem {
		case 0:
			m.mode = browseMode
			m.applyManualMatch()
		case 1:
			m.workspace.clearSelections()
			m.mode = browseMode
			m.status = "Line selection cleared."
		case 2:
			m.mode = browseMode
			m.resetComparisonState()
		default:
			m.mode = browseMode
			m.status = "Context menu closed."
		}
	case fileMenu:
		if m.menuItem == 0 {
			m.mode = browseMode
			m.beginPaste("")
		} else {
			return m, tea.Quit
		}
	case editMenu:
		switch m.menuItem {
		case 0:
			m.wrap = !m.wrap
			m.horizontal = [2]int{}
			m.clampViewport()
			if m.wrap {
				m.status = "Long lines wrap within each pane."
			} else {
				m.status = "Long lines use horizontal scrolling."
			}
			return m, nil
		case 1:
			m.mode = browseMode
			m.selectAllText()
		case 2:
			m.mode = browseMode
			return m.copyTextSelection()
		case 3:
			m.mode = browseMode
			return m.cutTextSelection()
		case 4:
			m.mode = browseMode
			m.pasteInternalClipboard()
		}
	case compareMenu:
		m.mode = browseMode
		switch m.menuItem {
		case 0:
			m.applyManualMatch()
		case 1:
			m.resetComparisonState()
		case 2:
			m.splitRatio = 0.5
			m.clampViewport()
			m.status = "Pane widths reset to an equal split."
		}
	case helpMenu:
		if m.menuItem == 0 {
			m.openHelp()
		} else {
			m.mode = browseMode
			m.status = "tcomp " + appVersion + " — editable side-by-side text comparison."
		}
	}
	return m, nil
}

func (m *appModel) syncCaretToCurrent() {
	sourceIndex := m.sourceIndexAt(m.focus, m.current)
	if sourceIndex < 0 {
		return
	}
	column := 0
	if m.caretActive {
		column = m.editor.col
	}
	lineLength := utf8.RuneCountInString(m.workspace.lines(m.focus)[sourceIndex])
	m.editor = editorState{side: m.focus, row: sourceIndex, col: max(0, min(lineLength, column))}
	m.caretActive = true
}

func (m *appModel) scrollBy(delta int) {
	m.scroll = max(0, min(m.maxScroll(), m.scroll+delta))
	rows := m.displayRows()
	if len(rows) == 0 {
		return
	}
	m.current = rows[min(m.scroll, len(rows)-1)].rowIndex
}

func (m *appModel) scrollHorizontal(which side, delta int) {
	m.horizontal[which] = max(0, min(m.maxHorizontal(which), m.horizontal[which]+delta))
}

func (m *appModel) keepEditorVisible() {
	m.clampViewport()
	which := m.editor.side
	prefix := string([]rune(m.editorLine())[:m.editor.col])
	cursorColumn := ansi.StringWidth(displayText(prefix))
	lineWidth := ansi.StringWidth(displayText(m.editorLine()))
	offset := m.horizontal[which]
	visible := m.visibleTextWidth(which, offset, lineWidth)
	if cursorColumn < offset {
		offset = cursorColumn
	} else if cursorColumn >= offset+visible {
		offset = cursorColumn - visible + 1
	}
	m.horizontal[which] = max(0, min(m.maxHorizontal(which), offset))
}

func (m *appModel) clampViewport() {
	if len(m.workspace.rows) == 0 {
		m.current, m.scroll = 0, 0
		m.horizontal = [2]int{}
		return
	}
	m.current = max(0, min(len(m.workspace.rows)-1, m.current))
	m.scroll = max(0, min(m.maxScroll(), m.scroll))
	for _, which := range []side{leftSide, rightSide} {
		m.horizontal[which] = max(0, min(m.maxHorizontal(which), m.horizontal[which]))
	}

	currentStart := m.visualRowIndex(m.current, 0)
	currentEnd := currentStart
	for index, row := range m.displayRows() {
		if row.rowIndex == m.current {
			currentEnd = index
		}
	}
	if currentStart < m.scroll {
		m.scroll = currentStart
	}
	if currentEnd >= m.scroll+m.bodyRows() {
		m.scroll = currentEnd - m.bodyRows() + 1
	}
	m.scroll = max(0, min(m.maxScroll(), m.scroll))
}

func (m appModel) bodyRows() int {
	return max(1, m.height-contentTop-2)
}

func (m appModel) maxScroll() int {
	return max(0, len(m.displayRows())-m.bodyRows())
}

func (m appModel) dividerX() int {
	available := max(0, m.width-dividerWidth)
	if available == 0 {
		return 0
	}
	ratio := m.splitRatio
	if ratio <= 0 || ratio >= 1 {
		ratio = 0.5
	}
	left := int(ratio*float64(available) + 0.5)
	if available >= minimumPaneWidth*2 {
		left = max(minimumPaneWidth, min(available-minimumPaneWidth, left))
	}
	return max(0, min(available, left))
}

func (m *appModel) setDividerX(x int) {
	available := max(0, m.width-dividerWidth)
	if available == 0 {
		m.splitRatio = 0.5
		return
	}
	if available >= minimumPaneWidth*2 {
		x = max(minimumPaneWidth, min(available-minimumPaneWidth, x))
	} else {
		x = max(0, min(available, x))
	}
	m.splitRatio = float64(x) / float64(available)
}

func (m appModel) paneWidth(which side) int {
	if which == rightSide {
		return max(0, m.width-dividerWidth-m.dividerX())
	}
	return m.dividerX()
}

func (m appModel) paneStart(which side) int {
	if which == rightSide {
		return m.dividerX() + dividerWidth
	}
	return 0
}

func (m appModel) dividerCell(x, y int) bool {
	return y >= 1 && y < m.height-1 && x >= m.dividerX() && x < m.dividerX()+dividerWidth
}

func (m appModel) gutterWidth() int {
	maximum := max(len(m.workspace.lines(leftSide)), len(m.workspace.lines(rightSide)))
	digits := len(fmt.Sprintf("%d", maximum))
	return max(7, digits+4)
}

func (m appModel) sideAtX(x int) (side, bool) {
	if x >= 0 && x < m.paneWidth(leftSide) {
		return leftSide, true
	}
	rightStart := m.paneStart(rightSide)
	if x >= rightStart && x < rightStart+m.paneWidth(rightSide) {
		return rightSide, true
	}
	return leftSide, false
}

func (m appModel) rowForSource(which side, sourceIndex int) int {
	for i, row := range m.workspace.rows {
		number := row.LeftNum
		if which == rightSide {
			number = row.RightNum
		}
		if number == sourceIndex+1 {
			return i
		}
	}
	return max(0, min(len(m.workspace.rows)-1, m.current))
}

func (m appModel) longestDisplayWidth(which side) int {
	longest := 0
	for _, line := range m.workspace.lines(which) {
		longest = max(longest, ansi.StringWidth(displayText(line)))
	}
	return longest
}

func (m appModel) visibleTextWidth(which side, offset, lineWidth int) int {
	width := max(1, m.paneWidth(which)-m.gutterWidth())
	if offset > 0 {
		width--
	}
	width = max(1, width)
	if lineWidth > offset+width {
		width--
	}
	return max(1, width)
}

func (m appModel) maxHorizontal(which side) int {
	if m.wrap {
		return 0
	}
	longest := m.longestDisplayWidth(which)
	available := max(1, m.paneWidth(which)-m.gutterWidth())
	if longest <= available {
		return 0
	}
	return max(0, longest+2-available)
}

func (m appModel) insertionIndex(which side, rowIndex int) int {
	for i := rowIndex + 1; i < len(m.workspace.rows); i++ {
		number := m.workspace.rows[i].LeftNum
		if which == rightSide {
			number = m.workspace.rows[i].RightNum
		}
		if number > 0 {
			return number - 1
		}
	}
	return len(m.workspace.lines(which))
}

func (m appModel) editorLine() string {
	lines := m.workspace.lines(m.editor.side)
	if m.editor.row < 0 || m.editor.row >= len(lines) {
		return ""
	}
	return lines[m.editor.row]
}

func normalizeNewlines(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.ReplaceAll(value, "\r", "\n")
}

func splitText(value string) []string {
	return normalizeLines(strings.Split(normalizeNewlines(value), "\n"))
}

func splitRunes(value string, position int) (string, string) {
	runes := []rune(value)
	position = max(0, min(len(runes), position))
	return string(runes[:position]), string(runes[position:])
}

func pasteLineStart(text []rune, cursor int) int {
	for cursor > 0 && text[cursor-1] != '\n' {
		cursor--
	}
	return cursor
}

func pasteLineEnd(text []rune, cursor int) int {
	for cursor < len(text) && text[cursor] != '\n' {
		cursor++
	}
	return cursor
}

func (m *appModel) movePasteCursorRows(delta int) {
	lines := strings.Split(string(m.paste.text), "\n")
	prefix := string(m.paste.text[:m.paste.cursor])
	row := strings.Count(prefix, "\n")
	column := utf8.RuneCountInString(prefix[strings.LastIndex(prefix, "\n")+1:])
	target := max(0, min(len(lines)-1, row+delta))
	cursor := 0
	for index := 0; index < target; index++ {
		cursor += utf8.RuneCountInString(lines[index]) + 1
	}
	m.paste.cursor = cursor + min(column, utf8.RuneCountInString(lines[target]))
}

func sideName(which side) string {
	if which == leftSide {
		return "Left"
	}
	return "Right"
}

func displayText(value string) string {
	var displayed strings.Builder
	for _, character := range value {
		displayed.WriteString(displayRune(character))
	}
	return displayed.String()
}

func displayRune(character rune) string {
	switch {
	case character == '\t':
		return "    "
	case unicode.IsControl(character):
		if character <= 0xff {
			return fmt.Sprintf("\\x%02X", character)
		}
		return fmt.Sprintf("\\u%04X", character)
	default:
		return string(character)
	}
}
