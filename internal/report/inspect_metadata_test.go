package report

import (
	"reflect"
	"testing"

	inspection "github.com/frithuz/mdchk/internal/inspect"
	"github.com/frithuz/mdchk/internal/result"
)

func TestMetadataCauseLinksFollowReportTrimming(t *testing.T) {
	p := inspection.Projection{Metadata: inspection.Section[inspection.MetadataRecord]{CauseIDs: []string{"interruption-1", "coverage-1"}}}
	retainInspectionCauses(&p, result.Result{Interruptions: []result.Interruption{{ID: "interruption-1"}}})
	if !reflect.DeepEqual(p.Metadata.CauseIDs, []string{"interruption-1"}) {
		t.Fatal(p.Metadata.CauseIDs)
	}
	clearInspectionCauses(&p)
	if p.Metadata.CauseIDs != nil {
		t.Fatal(p.Metadata.CauseIDs)
	}
}
