package media

import (
	"fmt"
	"github.com/DituLin/Atrium/internal/jobs"
	"github.com/DituLin/Atrium/internal/source"
	"testing"
)

func TestSourceStallDefersWithoutSpendingPhotoRetryBudget(t *testing.T) {
	for _, err := range []error{source.ErrStuck, source.ErrDegraded} {
		classified := classifyIOError(fmt.Errorf("open: %w", err))
		if !jobs.IsDeferred(classified) {
			t.Errorf("source stall must defer, got %v", classified)
		}
	}
}
