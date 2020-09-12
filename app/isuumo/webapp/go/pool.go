package main

import (
	geo "github.com/kellydunn/golang-geo"
	"sync"
)

// 変更禁止
var constEmptyEstates = []Estate{}
var constEmptyChairs = []Chair{}

// []Estateのプール
var estateSlicePool = sync.Pool{New: func() interface{} {
	return []Estate{}
}}

func getEmptyEstateSlice() []Estate {
	return estateSlicePool.Get().([]Estate)
}

func releaseEstateSlice(s []Estate) {
	estateSlicePool.Put(s[:0])
}

// []Chairのプール
var chairSlicePool = sync.Pool{New: func() interface{} {
	return []Chair{}
}}

func getEmptyChairSlice() []Chair {
	return chairSlicePool.Get().([]Chair)
}

func releaseChairSlice(s []Chair) {
	chairSlicePool.Put(s[:0])
}

// []*geo.Pointのプール
var geoPointsPool = sync.Pool{New: func() interface{} {
	return []*geo.Point{}
}}

func getEmptyGeoPointSlice() []*geo.Point {
	return geoPointsPool.Get().([]*geo.Point)
}

func releaseGeoPointSlice(s []*geo.Point) {
	geoPointsPool.Put(s[:0])
}
