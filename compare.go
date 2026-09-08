package main

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
)

// RowKind describes the relationship between the two source lines in a row.
type RowKind int

const (
	RowEqual RowKind = iota
	RowChanged
	RowLeftOnly
	RowRightOnly
)

// Row is one aligned display row. Line numbers are one-based; zero denotes a
// placeholder on that side.
type Row struct {
	LeftNum  int
	RightNum int
	Left     string
	Right    string
	Kind     RowKind
	Manual   bool
}

// ManualMatch pins two zero-based source lines together. Manual matches must be
// unique and non-crossing so the comparison remains ordered.
type ManualMatch struct {
	Left  int
	Right int
}

// Segment is a text run in a word-level comparison.
type Segment struct {
	Text    string
	Changed bool
}

var (
	ErrMatchOutOfRange = errors.New("manual match is outside the document")
	ErrMatchConflict   = errors.New("manual match conflicts with an existing match")
	ErrMatchCrossing   = errors.New("manual matches cannot cross")
)

// AddManualMatch validates and inserts a manual line match. Re-adding the exact
// same pair is idempotent.
func AddManualMatch(matches []ManualMatch, candidate ManualMatch, leftCount, rightCount int) ([]ManualMatch, error) {
	if candidate.Left < 0 || candidate.Left >= leftCount || candidate.Right < 0 || candidate.Right >= rightCount {
		return nil, fmt.Errorf("%w: left %d, right %d", ErrMatchOutOfRange, candidate.Left+1, candidate.Right+1)
	}

	result := append([]ManualMatch(nil), matches...)
	for _, match := range result {
		if match == candidate {
			return result, nil
		}
		if match.Left == candidate.Left || match.Right == candidate.Right {
			return nil, fmt.Errorf("%w: a selected line is already matched", ErrMatchConflict)
		}
		if (match.Left < candidate.Left) != (match.Right < candidate.Right) {
			return nil, fmt.Errorf("%w: left %d and right %d", ErrMatchCrossing, candidate.Left+1, candidate.Right+1)
		}
	}

	result = append(result, candidate)
	sort.Slice(result, func(i, j int) bool { return result[i].Left < result[j].Left })
	return result, nil
}

// BuildRows aligns two documents around validated manual anchors.
func BuildRows(left, right []string, matches []ManualMatch) ([]Row, error) {
	anchors := make([]ManualMatch, 0, len(matches))
	for _, match := range matches {
		var err error
		anchors, err = AddManualMatch(anchors, match, len(left), len(right))
		if err != nil {
			return nil, err
		}
	}

	rows := make([]Row, 0, len(left)+len(right))
	leftStart, rightStart := 0, 0
	for _, anchor := range anchors {
		rows = append(rows, alignRange(left, right, leftStart, anchor.Left, rightStart, anchor.Right)...)
		rows = append(rows, makeRow(anchor.Left, anchor.Right, left[anchor.Left], right[anchor.Right], true))
		leftStart, rightStart = anchor.Left+1, anchor.Right+1
	}
	rows = append(rows, alignRange(left, right, leftStart, len(left), rightStart, len(right))...)
	return rows, nil
}

func alignRange(left, right []string, leftStart, leftEnd, rightStart, rightEnd int) []Row {
	pairs := lcsPairs(left[leftStart:leftEnd], right[rightStart:rightEnd])
	rows := make([]Row, 0, (leftEnd-leftStart)+(rightEnd-rightStart))
	leftIndex, rightIndex := leftStart, rightStart
	for _, pair := range pairs {
		pairLeft, pairRight := leftStart+pair[0], rightStart+pair[1]
		rows = append(rows, unmatchedRows(left, right, leftIndex, pairLeft, rightIndex, pairRight)...)
		rows = append(rows, makeRow(pairLeft, pairRight, left[pairLeft], right[pairRight], false))
		leftIndex, rightIndex = pairLeft+1, pairRight+1
	}
	return append(rows, unmatchedRows(left, right, leftIndex, leftEnd, rightIndex, rightEnd)...)
}

func unmatchedRows(left, right []string, leftStart, leftEnd, rightStart, rightEnd int) []Row {
	rows := make([]Row, 0, max(leftEnd-leftStart, rightEnd-rightStart))
	for leftStart < leftEnd || rightStart < rightEnd {
		switch {
		case leftStart < leftEnd && rightStart < rightEnd:
			rows = append(rows, makeRow(leftStart, rightStart, left[leftStart], right[rightStart], false))
			leftStart++
			rightStart++
		case leftStart < leftEnd:
			rows = append(rows, Row{LeftNum: leftStart + 1, Left: left[leftStart], Kind: RowLeftOnly})
			leftStart++
		default:
			rows = append(rows, Row{RightNum: rightStart + 1, Right: right[rightStart], Kind: RowRightOnly})
			rightStart++
		}
	}
	return rows
}

func makeRow(leftIndex, rightIndex int, left, right string, manual bool) Row {
	kind := RowChanged
	if left == right {
		kind = RowEqual
	}
	return Row{
		LeftNum: leftIndex + 1, RightNum: rightIndex + 1,
		Left: left, Right: right, Kind: kind, Manual: manual,
	}
}

func lcsPairs(left, right []string) [][2]int {
	lengths := make([][]int, len(left)+1)
	for i := range lengths {
		lengths[i] = make([]int, len(right)+1)
	}
	for i := len(left) - 1; i >= 0; i-- {
		for j := len(right) - 1; j >= 0; j-- {
			if left[i] == right[j] {
				lengths[i][j] = lengths[i+1][j+1] + 1
			} else {
				lengths[i][j] = max(lengths[i+1][j], lengths[i][j+1])
			}
		}
	}

	pairs := make([][2]int, 0, lengths[0][0])
	for i, j := 0, 0; i < len(left) && j < len(right); {
		if left[i] == right[j] {
			pairs = append(pairs, [2]int{i, j})
			i++
			j++
		} else if lengths[i+1][j] >= lengths[i][j+1] {
			i++
		} else {
			j++
		}
	}
	return pairs
}

var wordToken = regexp.MustCompile(`\s+|[\pL\pN_]+|[^\s\pL\pN_]`)

// WordDiff marks only tokens outside the common subsequence as changed.
func WordDiff(left, right string) ([]Segment, []Segment) {
	leftTokens := wordToken.FindAllString(left, -1)
	rightTokens := wordToken.FindAllString(right, -1)
	pairs := lcsPairs(leftTokens, rightTokens)
	leftStable := make([]bool, len(leftTokens))
	rightStable := make([]bool, len(rightTokens))
	for _, pair := range pairs {
		leftStable[pair[0]] = true
		rightStable[pair[1]] = true
	}
	return tokenSegments(leftTokens, leftStable), tokenSegments(rightTokens, rightStable)
}

func tokenSegments(tokens []string, stable []bool) []Segment {
	segments := make([]Segment, 0, len(tokens))
	for i, token := range tokens {
		changed := !stable[i]
		if len(segments) > 0 && segments[len(segments)-1].Changed == changed {
			segments[len(segments)-1].Text += token
		} else {
			segments = append(segments, Segment{Text: token, Changed: changed})
		}
	}
	return segments
}
