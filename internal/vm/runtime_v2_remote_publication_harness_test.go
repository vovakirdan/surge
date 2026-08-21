//go:build runtime_v2_pending

package vm_test

// The stand is assembled from four parts so no single file carries the whole
// program. The split is by MODE FAMILY rather than by line count: the prologue
// everything shares, then spawn, then immediate/anchored, then far select.

const remotePublicationHarness = remotePublicationHarnessCommon +
	remotePublicationHarnessSpawn +
	remotePublicationHarnessImmediate +
	remotePublicationHarnessFarSelect
