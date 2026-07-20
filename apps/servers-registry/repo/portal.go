package repo

import "context"

// PortalStore is the shared source of truth used by every registry replica.
// Catalog data is immutable for a version; placement operations must be atomic.
type PortalStore interface {
	DestinationMap(context.Context, uint32) (uint32, bool, error)
	ReplaceDestinations(context.Context, map[uint32]uint32) error
	InstanceMaps(context.Context) ([]uint32, error)
	ReplaceInstanceMaps(context.Context, []uint32) error
	Placement(context.Context, uint32, string, uint64, uint32) (string, error)
	Placements(context.Context, uint32, string, uint64, []uint32) (map[uint32]string, error)
	BindPlacement(context.Context, uint32, string, uint64, uint32, string) (string, error)
	ReplacePlacement(context.Context, uint32, string, uint64, uint32, string, string) (string, error)
	SetPlacement(context.Context, uint32, string, uint64, uint32, string) error
	DeletePlacement(context.Context, uint32, string, uint64, uint32) error
	DeletePlacements(context.Context, uint32, string, []uint64, uint32) error
	GroupPlacementCounts(context.Context, uint32, []uint32) (map[string]uint32, error)
}
