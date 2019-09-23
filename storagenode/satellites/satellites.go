// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package satellites

import (
	"context"
	"time"

	"storj.io/storj/pkg/storj"
)

//Status refers to the state of the relationship with a satellites
type Status = int

const (
	//Unexpected status should not be used for sanity checking
	Unexpected Status = iota
	//Normal status reflects a lack of graceful exit
	Normal
	//Exiting reflects an active graceful exit
	Exiting
	//ExitSucceeded reflects a graceful exit that succeeded
	ExitSucceeded
	//ExitFailed reflects a graceful exit that failed
	ExitFailed
)

//ExitProcess contains the status of a graceful exit
type ExitProcess struct {
	SatelliteID       storj.NodeID
	InitiatedAt       *time.Time
	FinishedAt        *time.Time
	StartingDiskUsage int64
	BytesDeleted      int64
	CompletionReceipt []byte
}

// DB works with satellite database
//
// architecture: Database
type DB interface {
	// InitiateGracefulExit updates the database to reflect the beginning of a graceful exit
	InitiateGracefulExit(ctx context.Context, satelliteID storj.NodeID, intitiatedAt time.Time, startingDiskUsage int64) error
	// UpdateGracefulExit increments the total bytes deleted during a graceful exit
	UpdateGracefulExit(ctx context.Context, satelliteID storj.NodeID, bytesDeleted int64) error
	// CompleteGracefulExit updates the database when a graceful exit is completed or failed
	CompleteGracefulExit(ctx context.Context, satelliteID storj.NodeID, finishedAt time.Time, exitStatus Status, completionReceipt []byte) error
	// ListGracefulExits lists all graceful exit records
	ListGracefulExits(ctx context.Context) ([]ExitProcess, error)
}
