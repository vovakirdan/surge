package carriergate

import (
	"strings"
	"testing"
)

const canonicalChannelSendReservationFixture = `package asyncrt
type Payload interface{}
type ChannelID uint64
type ChannelSendRoute uint8
type TaskID uint64
type Task[P Payload] struct { State P }
type Executor[P Payload] struct { tasks map[TaskID]*Task[P] }
type Channel[P Payload] struct { buf []P }
type ChannelSendReservation[P Payload] struct {
	exec *Executor[P]
	channel *Channel[P]
	id ChannelID
	route ChannelSendRoute
	receiver TaskID
	recvSeq uint64
	sender TaskID
	valid bool
}
type claimEnvelope[P Payload] struct { claim ChannelSendReservation[P] }
`

func TestStructuralOwnerCensusRecognizesExactChannelSendControlClaim(t *testing.T) {
	t.Run("exact control claim", func(t *testing.T) {
		findings := scanControlClaimFixture(t, canonicalChannelSendReservationFixture)
		assertNoFindingPrefix(t, findings, "ChannelSendReservation.")
		assertNoFindingPrefix(t, findings, "claimEnvelope.claim->")
	})

	t.Run("nested package path fails closed", func(t *testing.T) {
		root := buildFixtureTree(t, map[string][]byte{
			"internal/asyncrt/sidecar/channel_reservation.go": []byte(canonicalChannelSendReservationFixture),
		}, false)
		findings, err := Scan(root)
		if err != nil {
			t.Fatalf("scan nested channel-send claim: %v", err)
		}
		if !hasFindingPrefix(findings, "ChannelSendReservation.exec->") {
			t.Fatalf("nested package spoof became a control terminal: %+v", findings)
		}
	})

	tests := []struct {
		name    string
		old     string
		updated string
		prefix  string
	}{
		{
			name:    "payload field fails closed",
			old:     "\tvalid bool\n",
			updated: "\tpayload P\n\tvalid bool\n",
			prefix:  "ChannelSendReservation.payload->",
		},
		{
			name:    "payload interface fails closed",
			old:     "\tvalid bool\n",
			updated: "\tpayload Payload\n\tvalid bool\n",
			prefix:  "ChannelSendReservation.payload->",
		},
		{
			name:    "renamed declaration fails closed",
			old:     "ChannelSendReservation",
			updated: "RenamedChannelSendReservation",
			prefix:  "RenamedChannelSendReservation.exec->",
		},
		{
			name:    "weakened sequence fails closed",
			old:     "\trecvSeq uint64\n",
			updated: "\trecvSeq uint32\n",
			prefix:  "ChannelSendReservation.exec->",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := strings.ReplaceAll(canonicalChannelSendReservationFixture, test.old, test.updated)
			findings := scanControlClaimFixture(t, source)
			if !hasFindingPrefix(findings, test.prefix) {
				t.Fatalf("non-canonical control claim hid prefix %q: %+v", test.prefix, findings)
			}
		})
	}
}

func scanControlClaimFixture(t *testing.T, source string) []Finding {
	t.Helper()
	root := buildFixtureTree(t, map[string][]byte{
		"internal/asyncrt/channel_reservation.go": []byte(source),
	}, false)
	findings, err := Scan(root)
	if err != nil {
		t.Fatalf("scan channel-send control claim: %v", err)
	}
	return findings
}

func assertNoFindingPrefix(t *testing.T, findings []Finding, prefix string) {
	t.Helper()
	if hasFindingPrefix(findings, prefix) {
		t.Fatalf("exact control claim became an owner through %q: %+v", prefix, findings)
	}
}

func hasFindingPrefix(findings []Finding, prefix string) bool {
	for _, finding := range findings {
		if finding.Category == categoryAsyncAny && strings.HasPrefix(finding.Token, prefix) {
			return true
		}
	}
	return false
}
