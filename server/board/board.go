// Package board is a grid of 5-foot squares.
package board

import "fmt"

// Pos is a square on the Board.
type Pos struct{ X, Y int }

// Terrain of one square.
type Terrain int

const (
	Floor Terrain = iota
	Wall
	Difficult
)

// SquareFeet is the side of one square.
const SquareFeet = 5

// Board is a hand-built grid. Y grows downward.
type Board struct {
	w, h  int
	cells []Terrain
}

// Parse builds a Board from text rows: '.' floor, '#' wall, '~' Difficult Terrain.
func Parse(rows []string) (*Board, error) {
	if len(rows) == 0 {
		return nil, fmt.Errorf("board: no rows")
	}
	b := &Board{w: len(rows[0]), h: len(rows)}
	for y, row := range rows {
		if len(row) != b.w {
			return nil, fmt.Errorf("board: row %d has width %d, want %d", y, len(row), b.w)
		}
		for x, c := range row {
			switch c {
			case '.':
				b.cells = append(b.cells, Floor)
			case '#':
				b.cells = append(b.cells, Wall)
			case '~':
				b.cells = append(b.cells, Difficult)
			default:
				return nil, fmt.Errorf("board: unknown square %q at %d,%d", c, x, y)
			}
		}
	}
	return b, nil
}

// In reports whether p is on the Board.
func (b *Board) In(p Pos) bool { return p.X >= 0 && p.Y >= 0 && p.X < b.w && p.Y < b.h }

// At returns the terrain of p; off-board squares are walls.
func (b *Board) At(p Pos) Terrain {
	if !b.In(p) {
		return Wall
	}
	return b.cells[p.Y*b.w+p.X]
}

// EntryCost is the movement, in squares, to enter p (SRD "Playing the Game":
// 1 square, 2 for Difficult Terrain). Walls cannot be entered.
func (b *Board) EntryCost(p Pos) (int, bool) {
	switch b.At(p) {
	case Wall:
		return 0, false
	case Difficult:
		return 2, true
	}
	return 1, true
}

// CanStep reports whether a creature can step from a to the adjacent square c:
// the target is enterable and a diagonal step does not cross a wall corner.
func (b *Board) CanStep(a, c Pos) bool {
	if _, ok := b.EntryCost(c); !ok {
		return false
	}
	if a.X != c.X && a.Y != c.Y {
		if b.At(Pos{a.X, c.Y}) == Wall || b.At(Pos{c.X, a.Y}) == Wall {
			return false
		}
	}
	return true
}

// Distance is the grid range in squares between two squares (diagonals cost 1).
func Distance(a, b Pos) int {
	dx, dy := abs(a.X-b.X), abs(a.Y-b.Y)
	return max(dx, dy)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
