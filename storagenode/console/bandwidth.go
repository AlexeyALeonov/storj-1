// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package console

// BandwidthInfo stores all info about storage node bandwidth usage
type BandwidthInfo struct {
	Used      float64 `json:"used"`
	Available float64 `json:"available"`
}
