package main

import "fmt"

type side int

const (
	leftSide side = iota
	rightSide
)

type lineChange struct {
	Start    int
	Deleted  int
	Inserted int
}

type lineSelection struct {
	Start  int
	End    int
	Active bool
}

func newLineSelection(anchor, current int) lineSelection {
	if anchor > current {
		anchor, current = current, anchor
	}
	return lineSelection{Start: anchor, End: current, Active: true}
}

func (selection lineSelection) contains(index int) bool {
	return selection.Active && index >= selection.Start && index <= selection.End
}

func (selection lineSelection) count() int {
	if !selection.Active {
		return 0
	}
	return selection.End - selection.Start + 1
}

type workspace struct {
	documents [2][]string
	rows      []Row
	matches   []ManualMatch
	picked    [2]int
	selected  [2]lineSelection
}

func newWorkspace(left, right []string) workspace {
	w := workspace{
		documents: [2][]string{normalizeLines(left), normalizeLines(right)},
		picked:    [2]int{-1, -1},
	}
	_ = w.rebuild()
	return w
}

func (w *workspace) lines(which side) []string {
	return w.documents[which]
}

func (w *workspace) rebuild() error {
	rows, err := BuildRows(w.documents[leftSide], w.documents[rightSide], w.matches)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		rows = []Row{{Kind: RowEqual}}
	}
	w.rows = rows
	return nil
}

func (w *workspace) selectLine(which side, index int) bool {
	return w.selectRange(which, index, index)
}

func (w *workspace) selectRange(which side, anchor, current int) bool {
	if anchor < 0 || anchor >= len(w.documents[which]) || current < 0 || current >= len(w.documents[which]) {
		return false
	}
	w.selected[which] = newLineSelection(anchor, current)
	w.picked[which] = current
	return true
}

func (w *workspace) clearSelections() {
	w.picked = [2]int{-1, -1}
	w.selected = [2]lineSelection{}
}

func (w *workspace) isSelected(which side, index int) bool {
	return w.selected[which].contains(index)
}

func (w *workspace) addSelectedMatch() error {
	return w.addSelectedMatches()
}

func (w *workspace) addSelectedMatches() error {
	left := w.selected[leftSide]
	right := w.selected[rightSide]
	if !left.Active || !right.Active {
		return fmt.Errorf("select lines on both sides first")
	}
	if left.count() != right.count() {
		return fmt.Errorf("selected ranges must contain the same number of lines")
	}

	matches := append([]ManualMatch(nil), w.matches...)
	for offset := 0; offset < left.count(); offset++ {
		var err error
		matches, err = AddManualMatch(
			matches,
			ManualMatch{Left: left.Start + offset, Right: right.Start + offset},
			len(w.documents[leftSide]),
			len(w.documents[rightSide]),
		)
		if err != nil {
			return err
		}
	}

	rows, err := BuildRows(w.documents[leftSide], w.documents[rightSide], matches)
	if err != nil {
		return err
	}
	w.matches = matches
	w.rows = rows
	return nil
}

func (w *workspace) resetMatches() {
	w.matches = nil
	_ = w.rebuild()
}

func (w *workspace) replaceDocument(which side, lines []string) {
	w.documents[which] = normalizeLines(lines)
	w.matches = nil
	w.clearSelections()
	_ = w.rebuild()
}

// updateDocument applies a known line-range edit and moves unaffected manual
// anchors and selections with the changed line numbers. Anchors or selection
// endpoints inside an ambiguous merge or deletion are removed.
func (w *workspace) updateDocument(which side, lines []string, change lineChange) {
	w.documents[which] = normalizeLines(lines)
	adjusted := make([]ManualMatch, 0, len(w.matches))
	for _, match := range w.matches {
		position := match.Left
		if which == rightSide {
			position = match.Right
		}
		moved, keep := transformLine(position, change)
		if !keep {
			continue
		}
		if which == leftSide {
			match.Left = moved
		} else {
			match.Right = moved
		}
		adjusted = append(adjusted, match)
	}
	w.matches = adjusted

	if w.picked[which] >= 0 {
		moved, keep := transformLine(w.picked[which], change)
		if keep {
			w.picked[which] = moved
		} else {
			w.picked[which] = -1
		}
	}
	selection := w.selected[which]
	if selection.Active {
		start, keepStart := transformLine(selection.Start, change)
		end, keepEnd := transformLine(selection.End, change)
		if keepStart && keepEnd {
			w.selected[which] = newLineSelection(start, end)
		} else {
			w.selected[which] = lineSelection{}
			w.picked[which] = -1
		}
	}
	_ = w.rebuild()
}

func transformLine(position int, change lineChange) (int, bool) {
	if position < change.Start {
		return position, true
	}
	end := change.Start + change.Deleted
	if position >= end {
		return position + change.Inserted - change.Deleted, true
	}
	if change.Deleted == 1 && change.Inserted > 0 && position == change.Start {
		return change.Start, true
	}
	return 0, false
}

func normalizeLines(lines []string) []string {
	if len(lines) == 0 {
		return []string{""}
	}
	return append([]string(nil), lines...)
}
