//go:build runtime_v2_pending

package vm_test

// The stand is assembled from parts so no single file carries the whole
// program. The split is by MODE FAMILY rather than by line count: the prologue
// everything shares, the descriptors its payloads are moved and destroyed
// through, then spawn, then immediate/anchored, then far select and the far
// select rows about a payload's own ownership.

const remotePublicationHarness = remotePublicationHarnessCommon +
	remotePublicationHarnessDescriptors +
	remotePublicationHarnessSpawn +
	remotePublicationHarnessImmediate +
	remotePublicationHarnessFarSelect +
	remotePublicationHarnessFarSelectPayload
