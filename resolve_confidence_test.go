package enriche

import (
	"testing"

	"github.com/anatolykoptev/go-enriche/maps"
)

func TestConfidenceForMaps_RichData(t *testing.T) {
	od := &maps.OrgData{
		Name:    "Supramen",
		Phone:   "+7 (999) 123-45-67",
		Website: "https://supramen.rest",
	}
	if got := confidenceForMaps(od); got != confMedium {
		t.Errorf("confidenceForMaps(rich) = %q, want %q", got, confMedium)
	}
}

func TestConfidenceForMaps_PartialData(t *testing.T) {
	od := &maps.OrgData{
		Name:  "Supramen",
		Phone: "+7 (999) 123-45-67",
		// no website
	}
	if got := confidenceForMaps(od); got != confLow {
		t.Errorf("confidenceForMaps(partial) = %q, want %q", got, confLow)
	}
}

func TestConfidenceForMaps_NilData(t *testing.T) {
	if got := confidenceForMaps(nil); got != confLow {
		t.Errorf("confidenceForMaps(nil) = %q, want %q", got, confLow)
	}
}

func TestConfidenceForMaps_EmptyData(t *testing.T) {
	od := &maps.OrgData{}
	if got := confidenceForMaps(od); got != confLow {
		t.Errorf("confidenceForMaps(empty) = %q, want %q", got, confLow)
	}
}
