package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"flag"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	ansi "github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestPasteReplacePreviewIsVisibleImmediatelyAndAppliesWithCtrlS(t *testing.T) {
	m := sizedModel([]string{"old"}, []string{"right"})
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = updated.(appModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("first\nsecond"), Paste: true})
	m = updated.(appModel)

	if m.mode != pasteMode || string(m.paste.text) != "first\nsecond" {
		t.Fatalf("paste state = mode %v text %q", m.mode, string(m.paste.text))
	}
	if view := m.View(); !strings.Contains(view, "first") || !strings.Contains(view, "second") {
		t.Fatalf("paste was not rendered immediately:\n%s", view)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(appModel)
	if got := m.workspace.lines(leftSide); !equalLines(got, []string{"first", "second"}) {
		t.Fatalf("applied left text = %#v", got)
	}
}

func TestDirectTypingAndUnmarkedPasteInsertAtCaret(t *testing.T) {
	m := sizedModel([]string{"old"}, []string{"right"})
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	m = updated.(appModel)
	if m.mode != browseMode || !equalLines(m.workspace.lines(leftSide), []string{"eold"}) {
		t.Fatalf("direct typing result = mode %v text %#v", m.mode, m.workspace.lines(leftSide))
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("clipboard text")})
	m = updated.(appModel)
	if m.mode != browseMode || !equalLines(m.workspace.lines(leftSide), []string{"eclipboard textold"}) {
		t.Fatalf("unmarked paste result = mode %v text %#v", m.mode, m.workspace.lines(leftSide))
	}
}

func TestAlwaysOnEditorHandlesNewlineAndMultilinePaste(t *testing.T) {
	m := sizedModel([]string{"before"}, []string{"before"})
	m.editor.col = len([]rune("before"))

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(appModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("after\nlast"), Paste: true})
	m = updated.(appModel)
	if got := m.workspace.lines(leftSide); !equalLines(got, []string{"before", "after", "last"}) {
		t.Fatalf("left text after direct editing = %#v", got)
	}
	if m.mode != browseMode || m.editor.row != 2 || m.editor.col != len([]rune("last")) {
		t.Fatalf("editor state = mode %v row %d col %d", m.mode, m.editor.row, m.editor.col)
	}
}

func TestAlwaysOnEditorUsesModifiedCommandsAndKeepsPlainLettersEditable(t *testing.T) {
	m := sizedModel([]string{""}, []string{""})
	for _, character := range "qejkhlgGmpr? " {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{character}})
		m = updated.(appModel)
	}
	if got, want := m.workspace.lines(leftSide)[0], "qejkhlgGmpr? "; got != want {
		t.Fatalf("plain command letters = %q, want %q", got, want)
	}

	updated, command := m.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})
	if command == nil {
		t.Fatal("Ctrl+Q did not request quit")
	}
	m = updated.(appModel)
	if got := m.workspace.lines(leftSide)[0]; got != "qejkhlgGmpr? " {
		t.Fatalf("Ctrl+Q changed document to %q", got)
	}
}

func TestLeftDragSelectsCharactersWithoutSelectingMatchLines(t *testing.T) {
	m := sizedModel([]string{"one", "two", "three", "four"}, []string{"one", "two", "three", "four"})
	contentX := m.gutterWidth()
	press := tea.MouseMsg{X: contentX + 1, Y: contentTop + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	updated, _ := m.Update(press)
	m = updated.(appModel)

	motion := tea.MouseMsg{X: contentX + 3, Y: contentTop + 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion}
	updated, _ = m.Update(motion)
	m = updated.(appModel)
	if !m.textSelection.active || m.textSelection.side != leftSide {
		t.Fatalf("text selection = %#v", m.textSelection)
	}
	if got := m.selectedText(); got != "wo\nthree\nfou" {
		t.Fatalf("selected text = %q, want %q", got, "wo\\nthree\\nfou")
	}
	if m.workspace.selected[leftSide].Active || m.workspace.picked[leftSide] != -1 {
		t.Fatalf("left drag changed manual line selection: selected=%#v picked=%d", m.workspace.selected[leftSide], m.workspace.picked[leftSide])
	}

	updated, _ = m.Update(tea.MouseMsg{X: contentX + 3, Y: contentTop + 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	m = updated.(appModel)
	if m.drag.active {
		t.Fatal("drag remained active after mouse release")
	}
}

func TestRightClickMenuSupportsHoverAndClickToMatchDraggedRanges(t *testing.T) {
	m := sizedModel([]string{"left one", "left two"}, []string{"right one", "right two"})
	m.workspace.selectRange(leftSide, 0, 1)
	m.workspace.selectRange(rightSide, 0, 1)

	rightClick := tea.MouseMsg{X: m.paneWidth(leftSide) + 8, Y: contentTop, Button: tea.MouseButtonRight, Action: tea.MouseActionPress}
	updated, _ := m.Update(rightClick)
	m = updated.(appModel)
	if m.mode != menuMode {
		t.Fatalf("right click opened mode %v, want menuMode", m.mode)
	}
	updated, _ = m.Update(tea.MouseMsg{X: rightClick.X, Y: rightClick.Y, Button: tea.MouseButtonRight, Action: tea.MouseActionRelease})
	m = updated.(appModel)
	if m.mode != menuMode {
		t.Fatal("right-button release closed the context menu")
	}
	menuX, menuY, _, _ := m.contextMenuRect()
	if !strings.Contains(m.View(), "Match 2 selected line pairs") {
		t.Fatalf("context menu does not describe the selected ranges:\n%s", m.View())
	}

	hoverClear := tea.MouseMsg{X: menuX + 2, Y: menuY + 2, Action: tea.MouseActionMotion}
	updated, _ = m.Update(hoverClear)
	m = updated.(appModel)
	if m.menuItem != 1 {
		t.Fatalf("hovered menu item = %d, want 1", m.menuItem)
	}

	hoverMatch := tea.MouseMsg{X: menuX + 2, Y: menuY + 1, Action: tea.MouseActionMotion}
	updated, _ = m.Update(hoverMatch)
	m = updated.(appModel)
	clickMatch := tea.MouseMsg{X: menuX + 2, Y: menuY + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	updated, _ = m.Update(clickMatch)
	m = updated.(appModel)
	if len(m.workspace.matches) != 2 || m.mode != browseMode {
		t.Fatalf("range match result: matches=%#v mode=%v", m.workspace.matches, m.mode)
	}
	for index, row := range m.workspace.rows {
		if !row.Manual {
			t.Fatalf("row %d is not marked as a manual match: %#v", index, row)
		}
	}
}

func TestRightClickOutsideSelectionReplacesOnlyThatSidesSelection(t *testing.T) {
	m := sizedModel([]string{"one", "two"}, []string{"one", "two"})
	m.workspace.selectRange(leftSide, 0, 1)
	m.workspace.selectLine(rightSide, 0)

	rightClick := tea.MouseMsg{X: m.paneWidth(leftSide) + 8, Y: contentTop + 1, Button: tea.MouseButtonRight, Action: tea.MouseActionPress}
	updated, _ := m.Update(rightClick)
	m = updated.(appModel)
	left := m.workspace.selected[leftSide]
	right := m.workspace.selected[rightSide]
	if left.Start != 0 || left.End != 1 || right.Start != 1 || right.End != 1 {
		t.Fatalf("selections after right click: left=%#v right=%#v", left, right)
	}
	if m.mode != menuMode {
		t.Fatalf("right click opened mode %v, want menuMode", m.mode)
	}
}

func TestLineNumberGutterUsesColorStatusBarWithoutDiffSymbols(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })

	m := sizedModel([]string{"same", "left"}, []string{"same", "right"})
	equalRow := m.renderPaneRow(m.workspace.rows[0], 0, leftSide)
	changedRow := m.renderPaneRow(m.workspace.rows[1], 1, leftSide)

	if !strings.Contains(equalRow, "▌") || !strings.Contains(equalRow, "38;5;42") {
		t.Fatalf("equal row does not use the green status bar: %q", equalRow)
	}
	if !strings.Contains(changedRow, "▌") || !strings.Contains(changedRow, "38;5;203") {
		t.Fatalf("changed row does not use the red status bar: %q", changedRow)
	}
	for _, oldMarker := range []string{"=", "~", "<", ">"} {
		if strings.Contains(equalRow, oldMarker) || strings.Contains(changedRow, oldMarker) {
			t.Fatalf("gutter still contains legacy marker %q", oldMarker)
		}
	}
}

func TestLegendUsesRenderedColorSwatches(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })

	m := sizedModel([]string{"same"}, []string{"same"})
	legend := m.renderLegend()
	for _, label := range []string{"Same", "Line", "Word", "Manual"} {
		if !strings.Contains(legend, label) {
			t.Fatalf("legend missing %q: %q", label, legend)
		}
	}
	if !strings.Contains(legend, "\x1b[") || strings.Count(legend, "■") != 2 || !strings.Contains(legend, "48;5;88") {
		t.Fatalf("legend does not contain the foreground and background color swatches: %q", legend)
	}
	header := m.renderHeader()
	strippedHeader := ansi.Strip(header)
	for _, label := range []string{"File", "Edit", "Compare", "Help"} {
		if !strings.Contains(strippedHeader, label) {
			t.Fatalf("header does not contain the %s menu: %q", label, header)
		}
	}
	if !strings.Contains(strippedHeader, "│ File │ Edit │ Compare │ Help") {
		t.Fatalf("header menus are not separated by subtle dividers: %q", header)
	}
	if strings.Contains(header, "48;5;45") {
		t.Fatalf("header still uses the old full-width blue background: %q", header)
	}
	if !strings.HasSuffix(strings.TrimRight(ansi.Strip(header), " "), "Manual") {
		t.Fatalf("header does not keep the color legend at the right: %q", header)
	}
	if got := ansi.StringWidth(header); got != m.width {
		t.Fatalf("header width = %d, want %d", got, m.width)
	}
}

func TestStatusAndCommandBarUseTheFinalTwoRows(t *testing.T) {
	m := sizedModel([]string{"same"}, []string{"same"})
	m.status = "Ready."
	lines := strings.Split(m.View(), "\n")
	if len(lines) != m.height {
		t.Fatalf("rendered rows = %d, want %d", len(lines), m.height)
	}

	status := ansi.Strip(lines[len(lines)-2])
	commands := ansi.Strip(lines[len(lines)-1])
	if !strings.HasPrefix(status, "Ready.") {
		t.Fatalf("status is not on the penultimate row: %q", status)
	}
	for _, label := range []string{"Help", "Reset", "Replace", "Match", "Copy", "Quit"} {
		if !strings.Contains(commands, label) {
			t.Fatalf("command bar is missing %q: %q", label, commands)
		}
	}
	if !strings.Contains(commands, "Help │ Reset │ Replace │ Match │ Copy │ Quit") {
		t.Fatalf("command bar is not separated by subtle dividers: %q", commands)
	}
	if strings.Contains(lines[len(lines)-1], "48;5;45") {
		t.Fatalf("command bar still uses blue button backgrounds: %q", lines[len(lines)-1])
	}
	if got := ansi.StringWidth(lines[len(lines)-1]); got != m.width {
		t.Fatalf("command bar width = %d, want %d", got, m.width)
	}
	if got, want := m.bodyRows(), m.height-contentTop-2; got != want {
		t.Fatalf("body rows = %d, want %d", got, want)
	}
}

func TestRenderedComparisonRowsKeepExactWidthWithUnicodeAndANSI(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })

	m := sizedModel([]string{"café old 🌍"}, []string{"café new 🌍"})
	lines := strings.Split(m.View(), "\n")
	if len(lines) <= contentTop {
		t.Fatalf("view has too few lines: %q", m.View())
	}
	want := m.paneWidth(leftSide) + dividerWidth + m.paneWidth(rightSide)
	if got := ansi.StringWidth(lines[contentTop]); got != want {
		t.Fatalf("comparison row width = %d, want %d; row=%q", got, want, lines[contentTop])
	}
}

func TestBoundedTTYInputBreaksTheAmbiguous256ByteRead(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = reader.Close()
		_ = writer.Close()
	})
	if _, err := writer.WriteString(strings.Repeat("x", 256)); err != nil {
		t.Fatal(err)
	}

	input := boundedTTYInput{File: reader}
	buffer := make([]byte, 256)
	count, err := input.Read(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if count != maxTerminalRead {
		t.Fatalf("read %d bytes, want %d", count, maxTerminalRead)
	}
	if input.Fd() != reader.Fd() {
		t.Fatal("terminal file descriptor was not preserved")
	}
}

func sizedModel(left, right []string) appModel {
	m := newAppModel(left, right)
	m.width, m.height = 100, 20
	m.clampViewport()
	return m
}

func equalLines(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func TestTopMenusAndBottomCommandsAreClickable(t *testing.T) {
	m := sizedModel([]string{"left"}, []string{"right"})
	m.workspace.matches = []ManualMatch{{Left: 0, Right: 0}}
	if err := m.workspace.rebuild(); err != nil {
		t.Fatal(err)
	}
	m.workspace.selectLine(leftSide, 0)
	m.textSelection = textSelection{side: leftSide, anchor: textPoint{row: 0, col: 0}, head: textPoint{row: 0, col: 2}, active: true}

	var compareHit topMenuHit
	for _, hit := range m.buildHeader().menus {
		if hit.kind == compareMenu {
			compareHit = hit
		}
	}
	updated, _ := m.Update(tea.MouseMsg{X: compareHit.x, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = updated.(appModel)
	if m.mode != menuMode || m.menu.kind != compareMenu || !strings.Contains(m.View(), "Equal pane widths") {
		t.Fatalf("compare menu did not open: mode=%v kind=%v\n%s", m.mode, m.menu.kind, m.View())
	}

	var helpHit topMenuHit
	for _, hit := range m.buildHeader().menus {
		if hit.kind == helpMenu {
			helpHit = hit
		}
	}
	updated, _ = m.Update(tea.MouseMsg{X: helpHit.x, Y: 0, Action: tea.MouseActionMotion})
	m = updated.(appModel)
	if m.menu.kind != helpMenu || !strings.Contains(m.View(), "Mouse and keyboard help") {
		t.Fatalf("hover did not switch to the Help menu: kind=%v\n%s", m.menu.kind, m.View())
	}
	menuX, menuY, _, _ := m.contextMenuRect()
	updated, _ = m.Update(tea.MouseMsg{X: menuX + 2, Y: menuY + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = updated.(appModel)
	if m.mode != helpMode {
		t.Fatalf("help menu item opened mode %v, want helpMode", m.mode)
	}
	view := m.View()
	for _, text := range []string{"Mouse-first layout", "Top menus and bottom commands are clickable", "Alt+H: help", "[ Close ]"} {
		if !strings.Contains(view, text) {
			t.Fatalf("help overlay missing %q:\n%s", text, view)
		}
	}
	if strings.Contains(strings.ToUpper(view), "F1") {
		t.Fatalf("help still advertises F1:\n%s", view)
	}

	var resetHit commandHit
	for _, hit := range m.buildCommandBar().hits {
		if hit.action == commandReset {
			resetHit = hit
		}
	}
	updated, _ = m.Update(tea.MouseMsg{X: resetHit.x, Y: m.height - 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = updated.(appModel)
	if len(m.workspace.matches) != 0 || m.workspace.selected[leftSide].Active || m.textSelection.active || m.mode != browseMode {
		t.Fatalf("reset command left state behind: matches=%#v lines=%#v text=%#v mode=%v", m.workspace.matches, m.workspace.selected[leftSide], m.textSelection, m.mode)
	}
}

func TestTextSelectionHighlightsOnlySelectedCharacters(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })

	m := sizedModel([]string{"abcdef"}, []string{"abcdef"})
	m.textSelection = textSelection{side: leftSide, anchor: textPoint{row: 0, col: 1}, head: textPoint{row: 0, col: 4}, active: true}
	row := m.renderPaneRow(m.workspace.rows[0], 0, leftSide)
	if !strings.Contains(row, "48;5;24") {
		t.Fatalf("selected characters do not use the selection background: %q", row)
	}
	if got := m.selectedText(); got != "bcd" {
		t.Fatalf("selected text = %q, want bcd", got)
	}
}

func TestRuneIndexAtDisplayColumnHandlesWideCharactersAndTabs(t *testing.T) {
	text := "a🌍\tb"
	cases := map[int]int{0: 0, 1: 1, 2: 1, 3: 2, 6: 2, 7: 3, 8: 4}
	for column, want := range cases {
		if got := runeIndexAtDisplayColumn(text, column); got != want {
			t.Fatalf("column %d maps to rune %d, want %d", column, got, want)
		}
	}
}

func TestRightClickSelectedTextOpensClipboardMenu(t *testing.T) {
	m := sizedModel([]string{"select this text"}, []string{"other"})
	m.textSelection = textSelection{
		side: leftSide, anchor: textPoint{row: 0, col: 0}, head: textPoint{row: 0, col: 6}, active: true,
	}
	m.clipboard = "replacement"
	var terminal bytes.Buffer
	m.clipboardOut = &terminal

	contentX := m.gutterWidth() + 2
	updated, _ := m.Update(tea.MouseMsg{
		X: contentX, Y: contentTop, Button: tea.MouseButtonRight, Action: tea.MouseActionPress,
	})
	m = updated.(appModel)

	if m.mode != menuMode || m.menu.kind != textContextMenu {
		t.Fatalf("right-click selection opened mode=%v kind=%v", m.mode, m.menu.kind)
	}
	view := m.View()
	for _, item := range []string{"Copy selected text", "Cut selected text", "Paste over selected text"} {
		if !strings.Contains(view, item) {
			t.Fatalf("clipboard menu missing %q:\n%s", item, view)
		}
	}
	if m.workspace.selected[leftSide].Active {
		t.Fatal("opening the clipboard menu also selected a line for matching")
	}

	menuX, menuY, _, _ := m.contextMenuRect()
	updated, command := m.Update(tea.MouseMsg{
		X: menuX + 2, Y: menuY + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	m = updated.(appModel)
	if m.mode != browseMode || m.clipboard != "select" || command == nil {
		t.Fatalf("mouse copy result: mode=%v clipboard=%q command=%v", m.mode, m.clipboard, command != nil)
	}
	_ = command()
	if terminal.Len() == 0 {
		t.Fatal("mouse copy did not write the terminal clipboard")
	}
}

func TestCtrlCCopiesSelectionToInternalAndTerminalClipboards(t *testing.T) {
	m := sizedModel([]string{"copy me"}, []string{"other"})
	m.textSelection = textSelection{
		side: leftSide, anchor: textPoint{row: 0, col: 0}, head: textPoint{row: 0, col: 4}, active: true,
	}
	var terminal bytes.Buffer
	m.clipboardOut = &terminal

	updated, command := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(appModel)
	if m.clipboard != "copy" {
		t.Fatalf("internal clipboard = %q, want copy", m.clipboard)
	}
	if command == nil {
		t.Fatal("copy did not return a terminal clipboard command")
	}
	message := command()
	if result, ok := message.(clipboardWriteMsg); !ok || result.err != nil {
		t.Fatalf("clipboard command result = %#v", message)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte("copy"))
	if !strings.Contains(terminal.String(), "\x1b]52;c;"+encoded+"\x07") {
		t.Fatalf("terminal clipboard output = %q", terminal.String())
	}
	if m.status != "Copied 4 characters." {
		t.Fatalf("copy status = %q", m.status)
	}
}

func TestCtrlXCutsSelectionAcrossLines(t *testing.T) {
	m := sizedModel([]string{"alpha", "bravo", "tail"}, []string{"other"})
	m.textSelection = textSelection{
		side: leftSide, anchor: textPoint{row: 0, col: 2}, head: textPoint{row: 1, col: 3}, active: true,
	}
	m.clipboardOut = &bytes.Buffer{}

	updated, command := m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	m = updated.(appModel)
	if m.clipboard != "pha\nbra" {
		t.Fatalf("cut clipboard = %q", m.clipboard)
	}
	if got := m.workspace.lines(leftSide); !equalLines(got, []string{"alvo", "tail"}) {
		t.Fatalf("left document after cut = %#v", got)
	}
	if m.textSelection.active {
		t.Fatal("cut left the text selection active")
	}
	if command == nil {
		t.Fatal("cut did not return a terminal clipboard command")
	}
}

func TestCtrlVPastesInternalClipboardOverSelection(t *testing.T) {
	m := sizedModel([]string{"hello world"}, []string{"other"})
	m.clipboard = "terminal"
	m.textSelection = textSelection{
		side: leftSide, anchor: textPoint{row: 0, col: 6}, head: textPoint{row: 0, col: 11}, active: true,
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	m = updated.(appModel)
	if got := m.workspace.lines(leftSide); !equalLines(got, []string{"hello terminal"}) {
		t.Fatalf("left document after paste = %#v", got)
	}
	if m.textSelection.active {
		t.Fatal("paste left the replaced selection active")
	}
}

func TestRightClickOutsideTextSelectionKeepsLineMatchingMenu(t *testing.T) {
	m := sizedModel([]string{"selected", "match me"}, []string{"right"})
	m.textSelection = textSelection{
		side: leftSide, anchor: textPoint{row: 0, col: 0}, head: textPoint{row: 0, col: 4}, active: true,
	}

	updated, _ := m.Update(tea.MouseMsg{
		X: m.gutterWidth() + 2, Y: contentTop + 1, Button: tea.MouseButtonRight, Action: tea.MouseActionPress,
	})
	m = updated.(appModel)
	if m.mode != menuMode || m.menu.kind != lineContextMenu {
		t.Fatalf("right-click outside selection opened mode=%v kind=%v", m.mode, m.menu.kind)
	}
	if !strings.Contains(m.View(), "Match selected lines") {
		t.Fatalf("line-matching menu is missing:\n%s", m.View())
	}
}

func TestExternalPasteReplacesActiveSelectionImmediately(t *testing.T) {
	m := sizedModel([]string{"before old after"}, []string{"other"})
	m.textSelection = textSelection{
		side: leftSide, anchor: textPoint{row: 0, col: 7}, head: textPoint{row: 0, col: 10}, active: true,
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("new"), Paste: true})
	m = updated.(appModel)
	if got := m.workspace.lines(leftSide); !equalLines(got, []string{"before new after"}) {
		t.Fatalf("left document after external paste = %#v", got)
	}
	if m.mode != browseMode || m.textSelection.active {
		t.Fatalf("external selection paste left mode=%v selection=%#v", m.mode, m.textSelection)
	}
}

func TestRightClickTextMenuPastesOverSelection(t *testing.T) {
	m := sizedModel([]string{"old value"}, []string{"other"})
	m.clipboard = "new"
	m.textSelection = textSelection{
		side: leftSide, anchor: textPoint{row: 0, col: 0}, head: textPoint{row: 0, col: 3}, active: true,
	}

	updated, _ := m.Update(tea.MouseMsg{
		X: m.gutterWidth() + 1, Y: contentTop, Button: tea.MouseButtonRight, Action: tea.MouseActionPress,
	})
	m = updated.(appModel)
	menuX, menuY, _, _ := m.contextMenuRect()
	updated, _ = m.Update(tea.MouseMsg{
		X: menuX + 2, Y: menuY + 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	m = updated.(appModel)

	if got := m.workspace.lines(leftSide); !equalLines(got, []string{"new value"}) {
		t.Fatalf("left document after mouse paste = %#v", got)
	}
	if m.mode != browseMode || m.textSelection.active {
		t.Fatalf("mouse paste left mode=%v selection=%#v", m.mode, m.textSelection)
	}
}

func TestVersionIsReportedByCLIAndHeader(t *testing.T) {
	var standardOutput, errorOutput bytes.Buffer
	_, _, _, err := loadArguments([]string{"--version"}, &standardOutput, &errorOutput)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("version error = %v, want flag.ErrHelp", err)
	}
	if got, want := standardOutput.String(), "tcomp "+appVersion+"\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
	if errorOutput.Len() != 0 {
		t.Fatalf("version wrote to stderr: %q", errorOutput.String())
	}

	m := sizedModel([]string{"same"}, []string{"same"})
	if header := ansi.Strip(m.renderHeader()); !strings.Contains(header, "tcomp "+appVersion) {
		t.Fatalf("header does not show version %q: %q", appVersion, header)
	}
}

func TestMouseDragResizesDividerAndPreservesSafePaneWidths(t *testing.T) {
	m := sizedModel([]string{"left"}, []string{"right"})
	initialDivider := m.dividerX()
	press := tea.MouseMsg{
		X: initialDivider + 1, Y: contentTop, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	}
	updated, _ := m.Update(press)
	m = updated.(appModel)
	if !m.dividerDragging {
		t.Fatal("pressing the divider did not start a drag")
	}

	updated, _ = m.Update(tea.MouseMsg{
		X: 71, Y: contentTop, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion,
	})
	m = updated.(appModel)
	if got, want := m.paneWidth(leftSide), 70; got != want {
		t.Fatalf("left pane width during drag = %d, want %d", got, want)
	}
	if got, want := m.paneWidth(rightSide), m.width-dividerWidth-70; got != want {
		t.Fatalf("right pane width during drag = %d, want %d", got, want)
	}

	updated, _ = m.Update(tea.MouseMsg{
		X: 71, Y: contentTop, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease,
	})
	m = updated.(appModel)
	if m.dividerDragging {
		t.Fatal("divider drag remained active after release")
	}
	if !strings.Contains(m.status, "Pane widths adjusted") {
		t.Fatalf("divider release status = %q", m.status)
	}

	divider := m.dividerX()
	if _, ok := m.sideAtX(divider + 1); ok {
		t.Fatal("divider hit area was treated as pane content")
	}
	if side, ok := m.sideAtX(m.paneStart(rightSide)); !ok || side != rightSide {
		t.Fatalf("right pane start maps to side=%v ok=%v", side, ok)
	}

	updated, _ = m.Update(tea.MouseMsg{
		X: divider + 1, Y: contentTop, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	m = updated.(appModel)
	updated, _ = m.Update(tea.MouseMsg{
		X: 0, Y: contentTop, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion,
	})
	m = updated.(appModel)
	if m.paneWidth(leftSide) != minimumPaneWidth {
		t.Fatalf("left pane shrank to %d, minimum is %d", m.paneWidth(leftSide), minimumPaneWidth)
	}
	if m.paneWidth(rightSide) < minimumPaneWidth {
		t.Fatalf("right pane shrank below minimum: %d", m.paneWidth(rightSide))
	}

	for index, line := range strings.Split(m.View(), "\n") {
		if got := ansi.StringWidth(line); got != m.width {
			t.Fatalf("rendered line %d width = %d, want %d", index, got, m.width)
		}
	}
}

func TestClickPlacesVisibleCaretAtTextPosition(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })

	m := sizedModel([]string{"abcdef"}, []string{"abcdef"})
	click := tea.MouseMsg{
		X: m.gutterWidth() + 2, Y: contentTop, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	}
	updated, _ := m.Update(click)
	m = updated.(appModel)
	updated, _ = m.Update(tea.MouseMsg{
		X: click.X, Y: click.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease,
	})
	m = updated.(appModel)

	if !m.caretActive || m.editor.side != leftSide || m.editor.row != 0 || m.editor.col != 2 {
		t.Fatalf("caret after click: active=%v editor=%#v", m.caretActive, m.editor)
	}
	row := m.renderPaneRow(m.workspace.rows[0], 0, leftSide)
	if !strings.Contains(row, "▏") || !strings.Contains(row, "38;5;220") {
		t.Fatalf("browse caret does not use the thin yellow cursor style: %q", row)
	}
	if strings.Contains(row, "48;5;220") {
		t.Fatalf("browse caret still uses the thick yellow block background: %q", row)
	}
}

func TestExternalPasteWithoutSelectionInsertsAtCaret(t *testing.T) {
	m := sizedModel([]string{"abcdef"}, []string{"abcdef"})
	click := tea.MouseMsg{
		X: m.gutterWidth() + 3, Y: contentTop, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	}
	updated, _ := m.Update(click)
	m = updated.(appModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("XX"), Paste: true})
	m = updated.(appModel)

	if got := m.workspace.lines(leftSide); !equalLines(got, []string{"abcXXdef"}) {
		t.Fatalf("left document after unselected external paste = %#v", got)
	}
	if m.mode != browseMode || !m.caretActive || m.editor.row != 0 || m.editor.col != 5 {
		t.Fatalf("paste result: mode=%v active=%v editor=%#v", m.mode, m.caretActive, m.editor)
	}
}

func TestCtrlVPastesInternalClipboardAtCaretWithoutSelection(t *testing.T) {
	m := sizedModel([]string{"abcdef"}, []string{"abcdef"})
	m.clipboard = "++"
	m.caretActive = true
	m.editor = editorState{side: leftSide, row: 0, col: 1}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	m = updated.(appModel)
	if got := m.workspace.lines(leftSide); !equalLines(got, []string{"a++bcdef"}) {
		t.Fatalf("left document after unselected internal paste = %#v", got)
	}
	if m.mode != browseMode || m.editor.col != 3 {
		t.Fatalf("internal paste result: mode=%v editor=%#v", m.mode, m.editor)
	}
}

func TestCursorBlinksBetweenThinBarAndText(t *testing.T) {
	m := sizedModel([]string{"abcdef"}, []string{"abcdef"})
	m.caretActive = true
	m.cursorVisible = true
	m.editor = editorState{side: leftSide, row: 0, col: 2}

	visible := m.renderPaneRow(m.workspace.rows[0], 0, leftSide)
	if !strings.Contains(visible, "▏") {
		t.Fatalf("visible blink phase has no thin caret: %q", visible)
	}

	updated, command := m.Update(cursorBlinkMsg{})
	m = updated.(appModel)
	if m.cursorVisible {
		t.Fatal("cursor remained visible after a blink message")
	}
	if command == nil {
		t.Fatal("blink message did not schedule the next blink")
	}
	hidden := m.renderPaneRow(m.workspace.rows[0], 0, leftSide)
	if strings.Contains(hidden, "▏") || !strings.Contains(ansi.Strip(hidden), "abcdef") {
		t.Fatalf("hidden blink phase did not restore the text: %q", hidden)
	}

	updated, _ = m.Update(cursorBlinkMsg{})
	m = updated.(appModel)
	if !m.cursorVisible {
		t.Fatal("cursor did not become visible on the next blink message")
	}
	if m.Init() == nil {
		t.Fatal("Init did not schedule cursor blinking")
	}
}

func TestDividerHighlightClearsWhenAnotherAreaIsSelected(t *testing.T) {
	m := sizedModel([]string{"left"}, []string{"right"})
	dividerX := m.dividerX() + 1

	updated, _ := m.Update(tea.MouseMsg{
		X: dividerX, Y: contentTop, Action: tea.MouseActionMotion,
	})
	m = updated.(appModel)
	if !m.dividerHover {
		t.Fatal("divider did not highlight on hover")
	}

	updated, _ = m.Update(tea.MouseMsg{
		X: dividerX, Y: contentTop, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	m = updated.(appModel)
	updated, _ = m.Update(tea.MouseMsg{
		X: dividerX, Y: contentTop, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease,
	})
	m = updated.(appModel)
	if !m.dividerHover || m.dividerDragging {
		t.Fatalf("divider release state: hover=%v dragging=%v", m.dividerHover, m.dividerDragging)
	}

	updated, _ = m.Update(tea.MouseMsg{
		X: m.gutterWidth() + 1, Y: contentTop, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	m = updated.(appModel)
	if m.dividerHover || m.dividerDragging {
		t.Fatalf("divider stayed highlighted after selecting text: hover=%v dragging=%v", m.dividerHover, m.dividerDragging)
	}
	if strings.Contains(ansi.Strip(m.renderVerticalDivider()), "┃") {
		t.Fatalf("divider still renders its focused color after losing focus: %q", m.renderVerticalDivider())
	}
}

func TestHelpUsesAltHAndDoesNotBindF1(t *testing.T) {
	m := sizedModel([]string{"left"}, []string{"right"})
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyF1})
	m = updated.(appModel)
	if m.mode != browseMode {
		t.Fatalf("F1 opened mode %v, want browseMode", m.mode)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}, Alt: true})
	m = updated.(appModel)
	if m.mode != helpMode {
		t.Fatalf("Alt+H opened mode %v, want helpMode", m.mode)
	}
}

func TestLoadArgumentsRejectsInvalidUTF8(t *testing.T) {
	path := t.TempDir() + "/invalid.txt"
	if err := os.WriteFile(path, []byte{0xff, 0xfe}, 0o600); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	_, _, _, err := loadArguments([]string{path}, &output, &output)
	if err == nil || !strings.Contains(err.Error(), "not valid UTF-8") {
		t.Fatalf("invalid UTF-8 error = %v", err)
	}
}

func TestDisplayTextEscapesTerminalControlCharacters(t *testing.T) {
	if got, want := displayText("safe\x1b[31m\ttext"), "safe\\x1B[31m    text"; got != want {
		t.Fatalf("display text = %q, want %q", got, want)
	}
}

func TestHelpCloseButtonRemainsVisibleAtMinimumHeight(t *testing.T) {
	m := newAppModel([]string{"left"}, []string{"right"})
	m.width, m.height = minimumWidth, minimumHeight
	m.openHelp()

	lines := m.renderHelpLines()
	if got := len(lines); got != minimumHeight {
		t.Fatalf("help lines = %d, want %d", got, minimumHeight)
	}
	if !strings.Contains(ansi.Strip(lines[len(lines)-2]), "[ Close ]") {
		t.Fatalf("minimum-height help has no close button:\n%s", strings.Join(lines, "\n"))
	}
	helpX, helpY, width, height := m.helpRect()
	buttonX := helpX + 1 + (width-2-len("[ Close ]"))/2
	if !m.helpCloseAt(buttonX, helpY+height-2) {
		t.Fatal("visible close button is outside the mouse hit area")
	}
}

func TestLongLinesShowOverflowIndicatorsWithoutAffectingShortPane(t *testing.T) {
	longLine := strings.Repeat("0123456789", 8)
	m := sizedModel([]string{longLine}, []string{"short"})

	left := ansi.Strip(m.renderPaneRow(m.workspace.rows[0], 0, leftSide))
	right := ansi.Strip(m.renderPaneRow(m.workspace.rows[0], 0, rightSide))
	if !strings.Contains(left, "›") {
		t.Fatalf("long line has no right overflow indicator: %q", left)
	}
	if strings.Contains(right, "‹") || strings.Contains(right, "›") {
		t.Fatalf("short line unexpectedly has an overflow indicator: %q", right)
	}
	if status := ansi.Strip(m.renderStatusLine()); !strings.Contains(status, "Left cols 1–") || !strings.Contains(status, "/80") {
		t.Fatalf("status does not report the visible column range: %q", status)
	}
}

func TestHorizontalWheelScrollsOnlyThePaneUnderThePointer(t *testing.T) {
	line := strings.Repeat("abcdefghij", 8)
	m := sizedModel([]string{line}, []string{line})
	leftBefore := ansi.Strip(m.renderPaneRow(m.workspace.rows[0], 0, leftSide))
	rightBefore := ansi.Strip(m.renderPaneRow(m.workspace.rows[0], 0, rightSide))

	updated, _ := m.Update(tea.MouseMsg{
		X: m.paneStart(rightSide) + m.gutterWidth(), Y: contentTop,
		Button: tea.MouseButtonWheelRight, Action: tea.MouseActionPress,
	})
	m = updated.(appModel)

	leftAfter := ansi.Strip(m.renderPaneRow(m.workspace.rows[0], 0, leftSide))
	rightAfter := ansi.Strip(m.renderPaneRow(m.workspace.rows[0], 0, rightSide))
	if leftAfter != leftBefore {
		t.Fatalf("scrolling the right pane changed the left pane:\nbefore %q\nafter  %q", leftBefore, leftAfter)
	}
	if rightAfter == rightBefore || !strings.Contains(rightAfter, "‹") {
		t.Fatalf("right pane did not scroll or show left overflow:\nbefore %q\nafter  %q", rightBefore, rightAfter)
	}

	leftPoint, ok := m.textPointAt(m.gutterWidth(), contentTop)
	if !ok || leftPoint.col != 0 {
		t.Fatalf("left click after right scroll maps to point=%#v ok=%v, want column 0", leftPoint, ok)
	}
	rightPoint, ok := m.textPointAt(m.paneStart(rightSide)+m.gutterWidth(), contentTop)
	if !ok || rightPoint.col == 0 {
		t.Fatalf("right click after right scroll maps to point=%#v ok=%v, want a scrolled column", rightPoint, ok)
	}
}

func TestShiftWheelProvidesHorizontalScrollingFallback(t *testing.T) {
	line := strings.Repeat("abcdefghij", 8)
	m := sizedModel([]string{line}, []string{line})
	before := ansi.Strip(m.renderPaneRow(m.workspace.rows[0], 0, leftSide))

	updated, _ := m.Update(tea.MouseMsg{
		X: m.gutterWidth(), Y: contentTop, Shift: true,
		Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
	})
	m = updated.(appModel)
	after := ansi.Strip(m.renderPaneRow(m.workspace.rows[0], 0, leftSide))
	if after == before || !strings.Contains(after, "‹") {
		t.Fatalf("Shift+wheel did not scroll the hovered pane horizontally:\nbefore %q\nafter  %q", before, after)
	}
}

func TestPastePreviewKeepsCaretVisibleOnLongLines(t *testing.T) {
	m := sizedModel([]string{"left"}, []string{"right"})
	m.beginPaste(strings.Repeat("x", 120))

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "‹") || !strings.Contains(view, "▏") {
		t.Fatalf("long paste preview does not show the scrolled caret and overflow indicator:\n%s", view)
	}
	if !strings.Contains(view, "columns ") || !strings.Contains(view, "/120") {
		t.Fatalf("long paste preview does not report its visible columns:\n%s", view)
	}
}

func TestCaretAutoScrollsToLongLineEndWithoutMovingOtherPane(t *testing.T) {
	line := strings.Repeat("abcdefghij", 8)
	m := sizedModel([]string{line}, []string{line})
	rightBefore := ansi.Strip(m.renderPaneRow(m.workspace.rows[0], 0, rightSide))
	m.editor = editorState{side: leftSide, row: 0, col: len([]rune(line))}
	m.focus = leftSide
	m.caretActive = true
	m.cursorVisible = true
	m.keepEditorVisible()

	left := ansi.Strip(m.renderPaneRow(m.workspace.rows[0], 0, leftSide))
	if !strings.Contains(left, "‹") || !strings.Contains(left, "▏") || strings.Contains(left, "›") {
		t.Fatalf("caret was not kept visible at the end of the long line: %q", left)
	}
	rightAfter := ansi.Strip(m.renderPaneRow(m.workspace.rows[0], 0, rightSide))
	if rightAfter != rightBefore {
		t.Fatalf("following the left caret moved the right pane:\nbefore %q\nafter  %q", rightBefore, rightAfter)
	}
}

func TestPageAndBoundaryKeysNavigateTheEditor(t *testing.T) {
	lines := make([]string, 40)
	for index := range lines {
		lines[index] = "abcdef"
	}
	m := sizedModel(lines, lines)
	m.editor = editorState{side: leftSide, row: 20, col: 3}
	m.current = m.rowForSource(leftSide, 20)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = updated.(appModel)
	if got, want := m.editor.row, 20-m.bodyRows(); got != want {
		t.Fatalf("Page Up moved to row %d, want %d", got, want)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = updated.(appModel)
	if m.editor.row != 20 || m.editor.col != 3 {
		t.Fatalf("Page Down caret = row %d col %d, want row 20 col 3", m.editor.row, m.editor.col)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = updated.(appModel)
	if m.editor.col != 0 {
		t.Fatalf("Home column = %d, want 0", m.editor.col)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = updated.(appModel)
	if m.editor.col != 6 {
		t.Fatalf("End column = %d, want 6", m.editor.col)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlHome})
	m = updated.(appModel)
	if m.editor.row != 0 || m.editor.col != 0 || m.current != 0 {
		t.Fatalf("Ctrl+Home caret = row %d col %d current %d", m.editor.row, m.editor.col, m.current)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlEnd})
	m = updated.(appModel)
	if m.editor.row != len(lines)-1 || m.editor.col != 6 || m.current != len(lines)-1 {
		t.Fatalf("Ctrl+End caret = row %d col %d current %d", m.editor.row, m.editor.col, m.current)
	}
}

func TestPageAndBoundaryKeysNavigateTheReplacementPreview(t *testing.T) {
	m := sizedModel([]string{"left"}, []string{"right"})
	previewLines := make([]string, 30)
	for index := range previewLines {
		previewLines[index] = "abcdef"
	}
	m.beginPaste(strings.Join(previewLines, "\n"))
	end := len(m.paste.text)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = updated.(appModel)
	if m.paste.cursor >= end {
		t.Fatalf("Page Up did not move the preview cursor: %d", m.paste.cursor)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlHome})
	m = updated.(appModel)
	if m.paste.cursor != 0 {
		t.Fatalf("Ctrl+Home preview cursor = %d, want 0", m.paste.cursor)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlEnd})
	m = updated.(appModel)
	if m.paste.cursor != end {
		t.Fatalf("Ctrl+End preview cursor = %d, want %d", m.paste.cursor, end)
	}
}
