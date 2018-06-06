package util

import (
	"sort"
	"github.com/shopspring/decimal"
)

// Zero represents ... zero
var Zero, _ = decimal.Zero.Float64()

// Max returns the largest supplied argument.
func Max(x int, y int) int {
	if x > y { return x }
	return y
}

// Min returns the smallest supplied argument.
func Min(x int, y int) int {
	if x < y { return x }
	return y
}

func convertExchangeRate(
	currentRate, baseTargetRate float64, fromBase string, toBase string) {

}

type KV struct {
	Key string
	Value int
}

func SortMap(ss []*KV) []*KV {
	sort.Slice(ss, func(i, j int) bool {
		return ss[i].Value > ss[j].Value
	})
	return ss
}