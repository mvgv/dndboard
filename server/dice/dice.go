// Package dice provides the server's only source of randomness.
package dice

import (
	"fmt"
	"math/rand/v2"
)

// Roller rolls one die with the given number of sides, returning 1..sides.
type Roller interface {
	Roll(sides int) int
}

type seeded struct{ r *rand.Rand }

// Seeded returns a reproducible roller: the same seed gives the same rolls.
func Seeded(seed uint64) Roller {
	return &seeded{r: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))}
}

func (s *seeded) Roll(sides int) int { return s.r.IntN(sides) + 1 }

type fixed struct{ values []int }

// Fixed returns a roller that yields the given values in order, for scenario
// tests that need exact dice. It panics when exhausted or when a value does
// not fit the die, so a wrong script fails loudly.
func Fixed(values ...int) Roller { return &fixed{values: values} }

func (f *fixed) Roll(sides int) int {
	if len(f.values) == 0 {
		panic("dice.Fixed: out of scripted values")
	}
	v := f.values[0]
	f.values = f.values[1:]
	if v < 1 || v > sides {
		panic(fmt.Sprintf("dice.Fixed: scripted %d does not fit d%d", v, sides))
	}
	return v
}
