package main

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	ansi "github.com/charmbracelet/x/ansi"
)

var (
	titleStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255"))
	mutedStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	equalStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	changedLineStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	changedWordStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Background(lipgloss.Color("88")).Bold(true)
	manualStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)
	focusStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)
	cursorStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)
	textSelectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Background(lipgloss.Color("24"))
	menuBarStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	menuBarTitleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Bold(true)
	menuBarActive     = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true).Underline(true)
	barSeparatorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	commandBarStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	commandStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	commandActive     = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true).Underline(true)
	menuStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("235"))
	menuActiveStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Background(lipgloss.Color("240")).Bold(true)
)

func (m appModel) View() string {
	if m.width < minimumWidth || m.height < minimumHeight {
		return fmt.Sprintf(
			"tcomp needs a terminal of at least %d×%d; current size is %d×%d.",
			minimumWidth, minimumHeight, m.width, m.height,
		)
	}
	if m.mode == pasteMode {
		return m.renderPasteView()
	}

	var output strings.Builder
	output.WriteString(m.renderHeader() + "\n")
	output.WriteString(m.renderPaneHeader(leftSide) + m.renderVerticalDivider() + m.renderPaneHeader(rightSide) + "\n")
	output.WriteString(strings.Repeat("─", m.paneWidth(leftSide)) + m.renderHorizontalDivider() + strings.Repeat("─", m.paneWidth(rightSide)) + "\n")

	for visible := 0; visible < m.bodyRows(); visible++ {
		rowIndex := m.scroll + visible
		if rowIndex >= len(m.workspace.rows) {
			output.WriteString(strings.Repeat(" ", m.paneWidth(leftSide)) + m.renderVerticalDivider() + strings.Repeat(" ", m.paneWidth(rightSide)) + "\n")
			continue
		}
		row := m.workspace.rows[rowIndex]
		output.WriteString(m.renderPaneRow(row, rowIndex, leftSide))
		output.WriteString(m.renderVerticalDivider())
		output.WriteString(m.renderPaneRow(row, rowIndex, rightSide))
		output.WriteByte('\n')
	}

	output.WriteString(m.renderStatusLine() + "\n")
	output.WriteString(m.renderCommandBar())
	view := output.String()
	if m.mode == menuMode {
		return m.overlayContextMenu(view)
	}
	if m.mode == helpMode {
		return m.overlayHelp(view)
	}
	return view
}

func (m appModel) renderVerticalDivider() string {
	if m.dividerDragging || m.dividerHover {
		return focusStyle.Render(" ┃ ")
	}
	return mutedStyle.Render(" │ ")
}

func (m appModel) renderHorizontalDivider() string {
	if m.dividerDragging || m.dividerHover {
		return focusStyle.Render("─╂─")
	}
	return mutedStyle.Render("─┼─")
}

func (m appModel) renderStatusLine() string {
	ranges := m.horizontalRangeStatus()
	if ranges == "" {
		return fitANSI(mutedStyle.Render(ansi.Truncate(m.status, m.width, "")), m.width)
	}
	rangeText := focusStyle.Render(ranges)
	rangeWidth := ansi.StringWidth(rangeText)
	statusWidth := max(0, m.width-rangeWidth-1)
	statusText := mutedStyle.Render(ansi.Truncate(m.status, statusWidth, ""))
	gap := max(1, m.width-ansi.StringWidth(statusText)-rangeWidth)
	return fitANSI(statusText+strings.Repeat(" ", gap)+rangeText, m.width)
}

func (m appModel) horizontalRangeStatus() string {
	parts := make([]string, 0, 2)
	for _, which := range []side{leftSide, rightSide} {
		if m.maxHorizontal(which) == 0 {
			continue
		}
		total := m.longestDisplayWidth(which)
		offset := m.horizontal[which]
		visible := m.visibleTextWidth(which, offset, total)
		first := min(total, offset) + 1
		last := min(total, offset+visible)
		parts = append(parts, fmt.Sprintf("%s cols %d–%d/%d", sideName(which), first, last, total))
	}
	return strings.Join(parts, "  ")
}

type commandBarAction int

const (
	commandHelp commandBarAction = iota
	commandReset
	commandReplace
	commandMatch
	commandCopy
	commandQuit
)

type commandHit struct {
	action commandBarAction
	x      int
	width  int
}

type commandBarLayout struct {
	line string
	hits []commandHit
}

var commandBarItems = []struct {
	action commandBarAction
	label  string
}{
	{commandHelp, "Help"},
	{commandReset, "Reset"},
	{commandReplace, "Replace"},
	{commandMatch, "Match"},
	{commandCopy, "Copy"},
	{commandQuit, "Quit"},
}

func (m appModel) buildCommandBar() commandBarLayout {
	var line strings.Builder
	hits := make([]commandHit, 0, len(commandBarItems))
	padding := commandBarStyle.Render(" ")
	line.WriteString(padding)
	x := ansi.StringWidth(padding)
	for index, item := range commandBarItems {
		separator := ""
		if index > 0 {
			separator = barSeparatorStyle.Render("│")
		}
		style := commandStyle
		if item.action == commandHelp && m.mode == helpMode {
			style = commandActive
		}
		label := style.Render(" " + item.label + " ")
		separatorWidth := ansi.StringWidth(separator)
		width := ansi.StringWidth(label)
		if x+separatorWidth+width > m.width {
			break
		}
		line.WriteString(separator)
		x += separatorWidth
		hits = append(hits, commandHit{action: item.action, x: x, width: width})
		line.WriteString(label)
		x += width
	}
	if x < m.width {
		line.WriteString(commandBarStyle.Render(strings.Repeat(" ", m.width-x)))
	}
	return commandBarLayout{line: fitANSI(line.String(), m.width), hits: hits}
}

func (m appModel) renderCommandBar() string {
	return m.buildCommandBar().line
}

func (m appModel) commandBarActionAt(x int) (commandBarAction, bool) {
	for _, hit := range m.buildCommandBar().hits {
		if x >= hit.x && x < hit.x+hit.width {
			return hit.action, true
		}
	}
	return 0, false
}

type topMenuHit struct {
	kind  contextMenuKind
	x     int
	width int
}

type headerLayout struct {
	line  string
	menus []topMenuHit
}

var topMenuItems = []struct {
	kind  contextMenuKind
	label string
}{
	{fileMenu, "File"},
	{editMenu, "Edit"},
	{compareMenu, "Compare"},
	{helpMenu, "Help"},
}

func (m appModel) buildHeader() headerLayout {
	var left strings.Builder
	title := menuBarTitleStyle.Render(" tcomp " + appVersion + " ")
	left.WriteString(title)
	x := ansi.StringWidth(title)
	menus := make([]topMenuHit, 0, len(topMenuItems))
	for _, item := range topMenuItems {
		separator := barSeparatorStyle.Render("│")
		left.WriteString(separator)
		x += ansi.StringWidth(separator)

		style := menuBarStyle
		if m.mode == menuMode && m.menu.kind == item.kind {
			style = menuBarActive
		}
		label := style.Render(" " + item.label + " ")
		width := ansi.StringWidth(label)
		menus = append(menus, topMenuHit{kind: item.kind, x: x, width: width})
		left.WriteString(label)
		x += width
	}

	legend := m.renderLegend()
	if ansi.StringWidth(left.String())+1+ansi.StringWidth(legend) > m.width {
		legend = m.renderCompactLegend()
	}
	legendWidth := ansi.StringWidth(legend)
	gap := max(0, m.width-ansi.StringWidth(left.String())-legendWidth)
	line := left.String() + strings.Repeat(" ", gap)
	if legendWidth <= m.width-ansi.StringWidth(left.String()) {
		line += legend
	}
	return headerLayout{line: fitANSI(line, m.width), menus: menus}
}

func (m appModel) renderHeader() string {
	return m.buildHeader().line
}

func (m appModel) topMenuAt(x int) (contextMenuKind, bool) {
	for _, hit := range m.buildHeader().menus {
		if x >= hit.x && x < hit.x+hit.width {
			return hit.kind, true
		}
	}
	return 0, false
}

func (m appModel) topMenuX(kind contextMenuKind) int {
	for _, hit := range m.buildHeader().menus {
		if hit.kind == kind {
			return hit.x
		}
	}
	return 0
}

func (m appModel) renderLegend() string {
	parts := []string{
		equalStyle.Render("■") + menuBarStyle.Render(" Same"),
		changedLineStyle.Render("■") + menuBarStyle.Render(" Line"),
		changedWordStyle.Render("  ") + menuBarStyle.Render(" Word"),
		manualStyle.Render("◆") + menuBarStyle.Render(" Manual"),
	}
	return strings.Join(parts, menuBarStyle.Render("  "))
}

func (m appModel) renderCompactLegend() string {
	return equalStyle.Render("■") + menuBarStyle.Render(" ") +
		changedLineStyle.Render("■") + menuBarStyle.Render(" ") +
		changedWordStyle.Render("  ") + menuBarStyle.Render(" ") +
		manualStyle.Render("◆")
}

func (m appModel) renderPaneHeader(which side) string {
	marker := "  "
	style := mutedStyle
	if m.focus == which {
		marker = "▶ "
		style = focusStyle
	}
	label := fmt.Sprintf("%s%s text  (%d lines)", marker, sideName(which), len(m.workspace.lines(which)))
	return fitANSI(style.Render(label), m.paneWidth(which))
}

func (m appModel) renderPaneRow(row Row, rowIndex int, which side) string {
	lineNumber := row.LeftNum
	if which == rightSide {
		lineNumber = row.RightNum
	}

	current := rowIndex == m.current && which == m.focus
	picked := lineNumber > 0 && m.workspace.isSelected(which, lineNumber-1)
	cursorMarker := " "
	switch {
	case current:
		cursorMarker = "▶"
	case picked:
		cursorMarker = "●"
	}
	digits := m.gutterWidth() - 4
	number := strings.Repeat(" ", digits)
	if lineNumber > 0 {
		number = fmt.Sprintf("%*d", digits, lineNumber)
	}

	cursorIndicator := mutedStyle.Render(cursorMarker)
	lineNumberStyle := mutedStyle
	switch {
	case current:
		cursorIndicator = focusStyle.Render(cursorMarker)
		lineNumberStyle = focusStyle
	case picked:
		cursorIndicator = manualStyle.Render(cursorMarker)
		lineNumberStyle = manualStyle
	}
	gutter := cursorIndicator + renderRowStatus(row) + " " + lineNumberStyle.Render(number) + " "

	paneWidth := m.paneWidth(which)
	available := max(1, paneWidth-m.gutterWidth())
	cursor := -1
	if m.caretActive && m.cursorVisible && !m.textSelection.active && m.editor.side == which && lineNumber == m.editor.row+1 {
		cursor = m.editor.col
	}
	selectionStart, selectionEnd := -1, -1
	if start, end, ok := m.textSelectionRange(which, rowIndex, utf8.RuneCountInString(rowText(row, which))); ok {
		selectionStart, selectionEnd = start, end
	}
	styled := renderComparedText(row, which, cursor, selectionStart, selectionEnd)
	lineWidth := ansi.StringWidth(displayText(rowText(row, which)))
	if lineNumber == 0 {
		styled = changedLineStyle.Render("∅")
		lineWidth = 1
	}
	content := m.renderScrollableText(styled, which, lineWidth, available)
	return fitANSI(gutter+content, paneWidth)
}

func (m appModel) renderScrollableText(styled string, which side, lineWidth, available int) string {
	indicatorStyle := mutedStyle
	if which == m.focus {
		indicatorStyle = focusStyle
	}
	return renderScrollableANSI(styled, m.horizontal[which], lineWidth, available, indicatorStyle)
}

func renderScrollableANSI(styled string, offset, lineWidth, available int, indicatorStyle lipgloss.Style) string {
	available = max(1, available)
	contentWidth := available
	leftIndicator := ""
	if offset > 0 {
		leftIndicator = indicatorStyle.Render("‹")
		contentWidth--
	}
	contentWidth = max(1, contentWidth)

	rightIndicator := ""
	if lineWidth > offset+contentWidth {
		rightIndicator = indicatorStyle.Render("›")
		contentWidth--
	}
	contentWidth = max(1, contentWidth)

	visible := ansi.Cut(styled, offset, offset+contentWidth)
	return fitANSI(leftIndicator+visible+rightIndicator, available)
}

func rowText(row Row, which side) string {
	if which == rightSide {
		return row.Right
	}
	return row.Left
}

func renderComparedText(row Row, which side, cursor, selectionStart, selectionEnd int) string {
	text := rowText(row, which)
	if row.Kind != RowChanged {
		style := equalStyle
		if row.Kind != RowEqual {
			style = changedLineStyle
		}
		return renderSegmentsWithCursor([]Segment{{Text: text}}, []lipgloss.Style{style}, cursor, selectionStart, selectionEnd)
	}

	leftSegments, rightSegments := WordDiff(row.Left, row.Right)
	segments := leftSegments
	if which == rightSide {
		segments = rightSegments
	}
	styles := make([]lipgloss.Style, len(segments))
	for i, segment := range segments {
		styles[i] = changedLineStyle
		if segment.Changed {
			styles[i] = changedWordStyle
		}
	}
	return renderSegmentsWithCursor(segments, styles, cursor, selectionStart, selectionEnd)
}

func renderSegmentsWithCursor(segments []Segment, styles []lipgloss.Style, cursor, selectionStart, selectionEnd int) string {
	var output strings.Builder
	position := 0
	for index, segment := range segments {
		style := styles[index]
		for _, character := range []rune(segment.Text) {
			value := displayRune(character)
			if position == cursor {
				cursorWidth := max(1, ansi.StringWidth(value))
				output.WriteString(cursorStyle.Render("▏" + strings.Repeat(" ", cursorWidth-1)))
			} else if position >= selectionStart && position < selectionEnd {
				output.WriteString(textSelectedStyle.Render(value))
			} else {
				output.WriteString(style.Render(value))
			}
			position++
		}
	}
	if position == cursor {
		output.WriteString(cursorStyle.Render("▏"))
	}
	return output.String()
}

func renderRowStatus(row Row) string {
	if row.Manual {
		return manualStyle.Render("◆")
	}
	if row.Kind == RowEqual {
		return equalStyle.Render("▌")
	}
	return changedLineStyle.Render("▌")
}

const contextMenuWidth = 44

func (m appModel) contextMenuItems() []string {
	switch m.menu.kind {
	case textContextMenu:
		pasteLabel := "Paste over selected text"
		if m.clipboard == "" {
			pasteLabel = "Paste (tcomp clipboard is empty)"
		}
		return []string{
			"Copy selected text",
			"Cut selected text",
			pasteLabel,
			"Cancel",
		}
	case fileMenu:
		return []string{"Replace focused document", "Quit tcomp"}
	case editMenu:
		pasteLabel := "Paste from tcomp clipboard"
		if m.clipboard == "" {
			pasteLabel = "Paste (tcomp clipboard is empty)"
		}
		return []string{"Select all in focused pane", "Copy selection", "Cut selection", pasteLabel}
	case compareMenu:
		return []string{"Match picked lines", "Reset matches and selections", "Equal pane widths"}
	case helpMenu:
		return []string{"Mouse and keyboard help", "About tcomp " + appVersion}
	}

	left := m.workspace.selected[leftSide]
	right := m.workspace.selected[rightSide]
	matchLabel := "Match selected lines (select both sides)"
	if left.Active && right.Active {
		switch {
		case left.count() != right.count():
			matchLabel = "Match selected lines (range sizes differ)"
		case left.count() == 1:
			matchLabel = "Match selected line pair"
		default:
			matchLabel = fmt.Sprintf("Match %d selected line pairs", left.count())
		}
	}
	return []string{
		matchLabel,
		"Clear line selection",
		"Reset all manual matches",
		"Cancel",
	}
}

func (m appModel) contextMenuRect() (x, y, width, height int) {
	width = min(contextMenuWidth, m.width)
	height = len(m.contextMenuItems()) + 2
	x = max(0, min(m.menu.x, m.width-width))
	y = m.menu.y
	if y+height > m.height {
		y = m.menu.y - height + 1
	}
	y = max(0, min(y, m.height-height))
	return x, y, width, height
}

func (m appModel) menuItemAt(x, y int) (int, bool) {
	menuX, menuY, width, _ := m.contextMenuRect()
	itemCount := len(m.contextMenuItems())
	if x < menuX || x >= menuX+width || y <= menuY || y > menuY+itemCount {
		return 0, false
	}
	return y - menuY - 1, true
}

func (m appModel) renderContextMenuLines() []string {
	_, _, width, _ := m.contextMenuRect()
	innerWidth := max(1, width-2)
	items := m.contextMenuItems()
	lines := make([]string, 0, len(items)+2)
	lines = append(lines, menuStyle.Render("┌"+strings.Repeat("─", innerWidth)+"┐"))
	for index, item := range items {
		prefix := "  "
		style := menuStyle
		if index == m.menuItem {
			prefix = "▶ "
			style = menuActiveStyle
		}
		content := fitANSI(prefix+item, innerWidth)
		lines = append(lines, menuStyle.Render("│")+style.Render(content)+menuStyle.Render("│"))
	}
	lines = append(lines, menuStyle.Render("└"+strings.Repeat("─", innerWidth)+"┘"))
	return lines
}

func (m appModel) overlayContextMenu(view string) string {
	menuX, menuY, menuWidth, _ := m.contextMenuRect()
	lines := strings.Split(view, "\n")
	for offset, menuLine := range m.renderContextMenuLines() {
		lineIndex := menuY + offset
		if lineIndex < 0 || lineIndex >= len(lines) {
			continue
		}
		base := fitANSI(lines[lineIndex], m.width)
		prefix := ansi.Cut(base, 0, menuX)
		suffix := ansi.Cut(base, menuX+menuWidth, m.width)
		lines[lineIndex] = fitANSI(prefix+menuLine+suffix, m.width)
	}
	return strings.Join(lines, "\n")
}

const helpOverlayHeight = 19

func (m appModel) helpRect() (x, y, width, height int) {
	width = min(68, max(1, m.width-2))
	height = min(helpOverlayHeight, m.height)
	x = max(0, (m.width-width)/2)
	y = max(0, (m.height-height)/2)
	return x, y, width, height
}

func (m appModel) helpCloseAt(x, y int) bool {
	helpX, helpY, width, height := m.helpRect()
	button := "[ Close ]"
	buttonX := helpX + 1 + max(0, (width-2-len(button))/2)
	return y == helpY+height-2 && x >= buttonX && x < buttonX+len(button)
}

func (m appModel) renderHelpLines() []string {
	_, _, width, height := m.helpRect()
	innerWidth := max(1, width-2)
	content := []string{
		"Help",
		"Mouse-first layout",
		"  Top menus and bottom commands are clickable; no function keys",
		"  Click: caret   Drag: select   Right-click: context menu",
		"Editing",
		"  Type: insert   Enter: new line   Backspace/Delete: remove",
		"  Arrows: move   Page Up/Down: one screen",
		"  Home/End: line   Ctrl+Home/End: document   Tab: pane",
		"Clipboard",
		"  Ctrl+A: select pane   Ctrl+C: copy   Ctrl+X: cut",
		"  Ctrl+V: internal paste   Shift+Insert: external paste",
		"Comparison",
		"  Right-click: line match   Divider: drag   Shift+wheel: horizontal",
		"Keyboard backup",
		"  Ctrl+P: replace   Alt+M: match   Ctrl+R: reset",
		"  Alt+H: help   Esc: close/clear   Ctrl+Q: quit",
		"",
	}
	bodyRows := max(1, height-2)
	if len(content) > bodyRows {
		visible := max(0, bodyRows-2)
		content = append(append([]string(nil), content[:visible]...), "  More shortcuts: README.md", "")
	}
	lines := make([]string, 0, height)
	lines = append(lines, menuStyle.Render("┌"+strings.Repeat("─", innerWidth)+"┐"))
	for index, text := range content {
		style := menuStyle
		if index == 0 || text == "Mouse-first layout" || text == "Editing" || text == "Clipboard" || text == "Comparison" || text == "Keyboard backup" {
			style = menuStyle.Copy().Bold(true)
		}
		if index == len(content)-1 {
			button := "[ Close ]"
			left := max(0, (innerWidth-len(button))/2)
			right := max(0, innerWidth-left-len(button))
			lines = append(lines, menuStyle.Render("│"+strings.Repeat(" ", left))+commandActive.Render(button)+menuStyle.Render(strings.Repeat(" ", right)+"│"))
			continue
		}
		lines = append(lines, menuStyle.Render("│")+style.Render(fitANSI(text, innerWidth))+menuStyle.Render("│"))
	}
	lines = append(lines, menuStyle.Render("└"+strings.Repeat("─", innerWidth)+"┘"))
	return lines
}

func (m appModel) overlayHelp(view string) string {
	helpX, helpY, helpWidth, _ := m.helpRect()
	lines := strings.Split(view, "\n")
	for offset, helpLine := range m.renderHelpLines() {
		lineIndex := helpY + offset
		if lineIndex < 0 || lineIndex >= len(lines) {
			continue
		}
		base := fitANSI(lines[lineIndex], m.width)
		prefix := ansi.Cut(base, 0, helpX)
		suffix := ansi.Cut(base, helpX+helpWidth, m.width)
		lines[lineIndex] = fitANSI(prefix+helpLine+suffix, m.width)
	}
	return strings.Join(lines, "\n")
}

func (m appModel) renderPasteView() string {
	var output strings.Builder
	output.WriteString(titleStyle.Render("Paste and replace "+sideName(m.paste.target)+" text") + "\n")
	output.WriteString(mutedStyle.Render("Text appears as it arrives. Ctrl+S applies the replacement; Esc cancels.") + "\n")
	output.WriteString(strings.Repeat("─", m.width) + "\n")

	text := string(m.paste.text)
	lines := strings.Split(text, "\n")
	cursorLine, cursorColumn := pasteCursorPosition(m.paste.text, m.paste.cursor)
	bodyHeight := max(1, m.height-5)
	start := max(0, cursorLine-bodyHeight+1)
	if start > max(0, len(lines)-bodyHeight) {
		start = max(0, len(lines)-bodyHeight)
	}
	digits := max(2, len(fmt.Sprintf("%d", len(lines))))
	contentWidth := max(1, m.width-digits-2)
	cursorLineText := lines[min(cursorLine, len(lines)-1)]
	horizontal := pasteHorizontalOffset(cursorLineText, cursorColumn, contentWidth)
	for visible := 0; visible < bodyHeight; visible++ {
		lineIndex := start + visible
		if lineIndex >= len(lines) {
			output.WriteString(strings.Repeat(" ", m.width) + "\n")
			continue
		}
		gutter := mutedStyle.Render(fmt.Sprintf("%*d ", digits, lineIndex+1))
		cursor := -1
		if m.cursorVisible && lineIndex == cursorLine {
			cursor = cursorColumn
		}
		styled := renderPlainText(lines[lineIndex], cursor)
		lineWidth := ansi.StringWidth(displayText(lines[lineIndex]))
		content := renderScrollableANSI(styled, horizontal, lineWidth, contentWidth, focusStyle)
		output.WriteString(fitANSI(gutter+content, m.width) + "\n")
	}
	status := fmt.Sprintf("%d lines, %d characters", len(lines), utf8.RuneCountInString(text))
	lineWidth := ansi.StringWidth(displayText(cursorLineText))
	if lineWidth > contentWidth {
		visibleWidth := contentWidth
		if horizontal > 0 {
			visibleWidth--
		}
		visibleWidth = max(1, visibleWidth)
		if lineWidth > horizontal+visibleWidth {
			visibleWidth--
		}
		status += fmt.Sprintf(", columns %d–%d/%d", horizontal+1, min(lineWidth, horizontal+max(1, visibleWidth)), lineWidth)
	}
	output.WriteString(fitANSI(mutedStyle.Render(status), m.width))
	return output.String()
}

func pasteHorizontalOffset(line string, cursorRune, available int) int {
	lineWidth := ansi.StringWidth(displayText(line))
	available = max(1, available)
	if lineWidth <= available {
		return 0
	}
	runes := []rune(line)
	cursorRune = max(0, min(len(runes), cursorRune))
	cursorColumn := ansi.StringWidth(displayText(string(runes[:cursorRune])))
	visible := max(1, available-1)
	offset := 0
	if cursorColumn >= visible {
		offset = cursorColumn - visible + 1
	}
	return max(0, min(lineWidth+2-available, offset))
}

func renderPlainText(text string, cursor int) string {
	segment := Segment{Text: text}
	return renderSegmentsWithCursor([]Segment{segment}, []lipgloss.Style{titleStyle.Copy().Bold(false)}, cursor, -1, -1)
}

func pasteCursorPosition(text []rune, cursor int) (int, int) {
	line, column := 0, 0
	for index, character := range text {
		if index >= cursor {
			break
		}
		if character == '\n' {
			line, column = line+1, 0
		} else {
			column++
		}
	}
	return line, column
}

func fitANSI(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(value) > width {
		value = ansi.Truncate(value, width, "")
	}
	return value + strings.Repeat(" ", max(0, width-ansi.StringWidth(value)))
}
