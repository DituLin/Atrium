package store

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAnalysisFormulaReferenceBounds(t *testing.T) {
	for _, formula := range []string{"SUM(B2:B3)", "=SUM($B$2,$B$3)", "sum(B2:C3)"} {
		cells, ok := analysisFormulaCells(formula)
		require.True(t, ok, formula)
		require.NotEmpty(t, cells)
	}
	for _, formula := range []string{"SUM(Sheet2!B2:B3)", "SUM([a.xlsx]S!B2:B3)", "SUM(B2,B2)", "SUM(B3:B2)", "SUM(B1:B999999)", "SUM(XFE1:XFE2)", "SUM(B0:B2)", "SUM(B2:B3)+1", "SUM(INDIRECT(B2))", "SUM(B2:B3;D2)", "SUM(B2)", "SUM(2)"} {
		_, ok := analysisFormulaCells(formula)
		if formula == "SUM(B2)" {
			require.True(t, ok)
		} else {
			require.False(t, ok, formula)
		}
	}
}
