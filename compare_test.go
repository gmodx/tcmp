package main

import (
	"errors"
	"testing"
)

func TestBuildRowsAlignsEqualChangedAndInsertedLines(t *testing.T) {
	rows, err := BuildRows(
		[]string{"alpha", "before", "omega"},
		[]string{"alpha", "after", "inserted", "omega"},
		nil,
	)
	if err != nil {
		t.Fatalf("BuildRows returned an error: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("row count = %d, want 4", len(rows))
	}
	if rows[0].Kind != RowEqual || rows[3].Kind != RowEqual {
		t.Fatalf("equal rows were not preserved: %#v", rows)
	}
	if rows[1].Kind != RowChanged || rows[1].LeftNum != 2 || rows[1].RightNum != 2 {
		t.Fatalf("replacement row = %#v, want paired line 2", rows[1])
	}
	if rows[2].Kind != RowRightOnly || rows[2].LeftNum != 0 || rows[2].RightNum != 3 {
		t.Fatalf("inserted row = %#v, want a left placeholder", rows[2])
	}
}

func TestManualMatchAnchorsOtherwiseMovedLines(t *testing.T) {
	left := []string{"start", "target", "tail"}
	right := []string{"start", "tail", "target"}
	rows, err := BuildRows(left, right, []ManualMatch{{Left: 1, Right: 2}})
	if err != nil {
		t.Fatalf("BuildRows returned an error: %v", err)
	}

	for _, row := range rows {
		if row.LeftNum == 2 && row.RightNum == 3 {
			if !row.Manual || row.Kind != RowEqual {
				t.Fatalf("manual row = %#v, want an equal manual anchor", row)
			}
			return
		}
	}
	t.Fatal("manual anchor row was not emitted")
}

func TestAddManualMatchRejectsConflictsAndCrossings(t *testing.T) {
	matches := []ManualMatch{{Left: 1, Right: 1}}

	if _, err := AddManualMatch(matches, ManualMatch{Left: 1, Right: 2}, 4, 4); !errors.Is(err, ErrMatchConflict) {
		t.Fatalf("same-line conflict error = %v, want ErrMatchConflict", err)
	}
	if _, err := AddManualMatch(matches, ManualMatch{Left: 2, Right: 0}, 4, 4); !errors.Is(err, ErrMatchCrossing) {
		t.Fatalf("crossing error = %v, want ErrMatchCrossing", err)
	}
	if _, err := AddManualMatch(matches, ManualMatch{Left: 5, Right: 2}, 4, 4); !errors.Is(err, ErrMatchOutOfRange) {
		t.Fatalf("range error = %v, want ErrMatchOutOfRange", err)
	}
}

func TestAddManualMatchSortsAndDeduplicates(t *testing.T) {
	matches, err := AddManualMatch([]ManualMatch{{Left: 3, Right: 3}}, ManualMatch{Left: 1, Right: 1}, 5, 5)
	if err != nil {
		t.Fatalf("AddManualMatch returned an error: %v", err)
	}
	matches, err = AddManualMatch(matches, ManualMatch{Left: 1, Right: 1}, 5, 5)
	if err != nil {
		t.Fatalf("re-adding a match returned an error: %v", err)
	}
	if len(matches) != 2 || matches[0] != (ManualMatch{Left: 1, Right: 1}) {
		t.Fatalf("matches = %#v, want sorted unique matches", matches)
	}
}

func TestWordDiffMarksOnlyChangedTokens(t *testing.T) {
	left, right := WordDiff("hello old world", "hello new world")
	if joinedSegments(left) != "hello old world" || joinedSegments(right) != "hello new world" {
		t.Fatalf("word diff changed source text: left=%#v right=%#v", left, right)
	}
	if !hasChangedSegment(left, "old") || !hasChangedSegment(right, "new") {
		t.Fatalf("replacement tokens were not marked: left=%#v right=%#v", left, right)
	}
	if hasChangedSegment(left, "hello") || hasChangedSegment(right, "world") {
		t.Fatalf("stable tokens were marked: left=%#v right=%#v", left, right)
	}
}

func TestWorkspaceMovesAnchorsAfterLineInsertion(t *testing.T) {
	w := newWorkspace([]string{"one", "two", "three"}, []string{"one", "two", "three"})
	w.matches = []ManualMatch{{Left: 2, Right: 2}}
	if err := w.rebuild(); err != nil {
		t.Fatal(err)
	}

	w.updateDocument(leftSide, []string{"one", "new", "two", "three"}, lineChange{Start: 1, Deleted: 0, Inserted: 1})
	if len(w.matches) != 1 || w.matches[0] != (ManualMatch{Left: 3, Right: 2}) {
		t.Fatalf("matches after insertion = %#v, want shifted left anchor", w.matches)
	}
}

func TestWorkspaceDropsAmbiguousAnchorsAfterMerge(t *testing.T) {
	w := newWorkspace([]string{"one", "two", "three"}, []string{"one", "two", "three"})
	w.matches = []ManualMatch{{Left: 1, Right: 1}, {Left: 2, Right: 2}}
	if err := w.rebuild(); err != nil {
		t.Fatal(err)
	}

	w.updateDocument(leftSide, []string{"one", "twothree"}, lineChange{Start: 1, Deleted: 2, Inserted: 1})
	if len(w.matches) != 0 {
		t.Fatalf("matches after merge = %#v, want ambiguous anchors removed", w.matches)
	}
}

func TestReplacingDocumentClearsManualState(t *testing.T) {
	w := newWorkspace([]string{"left"}, []string{"right"})
	w.matches = []ManualMatch{{Left: 0, Right: 0}}
	w.picked = [2]int{0, 0}
	w.replaceDocument(leftSide, []string{"replacement"})
	if len(w.matches) != 0 || w.picked != [2]int{-1, -1} {
		t.Fatalf("manual state after replacement: matches=%#v picked=%#v", w.matches, w.picked)
	}
}

func TestWorkspaceRangeMatchIsAtomicWhenRangeSizesDiffer(t *testing.T) {
	w := newWorkspace([]string{"left one", "left two"}, []string{"right one", "right two"})
	w.selectRange(leftSide, 0, 1)
	w.selectLine(rightSide, 0)

	if err := w.addSelectedMatches(); err == nil {
		t.Fatal("matching unequal ranges succeeded")
	}
	if len(w.matches) != 0 {
		t.Fatalf("unequal range match partially mutated matches: %#v", w.matches)
	}
}

func TestWorkspaceMovesDragSelectionAfterLineInsertion(t *testing.T) {
	w := newWorkspace([]string{"one", "two", "three"}, []string{"one", "two", "three"})
	w.selectRange(leftSide, 1, 2)

	w.updateDocument(leftSide, []string{"new", "one", "two", "three"}, lineChange{Start: 0, Inserted: 1})
	selection := w.selected[leftSide]
	if !selection.Active || selection.Start != 2 || selection.End != 3 || w.picked[leftSide] != 3 {
		t.Fatalf("selection after insertion = %#v, picked=%d", selection, w.picked[leftSide])
	}
}
func joinedSegments(segments []Segment) string {
	var result string
	for _, segment := range segments {
		result += segment.Text
	}
	return result
}

func hasChangedSegment(segments []Segment, text string) bool {
	for _, segment := range segments {
		if segment.Changed && segment.Text == text {
			return true
		}
	}
	return false
}
