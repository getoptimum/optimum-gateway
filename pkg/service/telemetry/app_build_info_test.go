package telemetry

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/logger"
)

// A call before metrics exist does nothing. The same type keeps the one series,
// and a new type replaces it.
func TestSetAppBuildPairedWithReplacesSeries(t *testing.T) {
	reg := initTestMetricsRegistry(t)
	name := testMetricsNamespace + "_" + testMetricsSubsystem + "_app_build_info"

	SetAppBuildPairedWith("relay")
	InitMetricsWithRegistry(logger.NewAppSLogger(logger.Debug), "partner")

	check := func(pairedWith string) {
		t.Helper()
		require.Len(t, metricFamilyByName(t, reg, name).Metric, 1)
		require.Equal(t, 1.0, metricByLabels(t, reg, name, map[string]string{"paired_with": pairedWith}).GetGauge().GetValue())
	}

	check("partner")
	SetAppBuildPairedWith("partner")
	check("partner")
	SetAppBuildPairedWith("relay")
	check("relay")
}
