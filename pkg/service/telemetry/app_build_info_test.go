package telemetry

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/logger"
)

func TestSetAppBuildPairedWithReplacesSeries(t *testing.T) {
	reg := initTestMetricsRegistry(t)
	InitMetricsWithRegistry(logger.NewAppSLogger(logger.Debug), "partner")
	t.Cleanup(func() {
		appBuildInfo = nil
		appBuildBaseLabels = nil
		appBuildPairedWith = ""
	})

	name := testMetricsNamespace + "_" + testMetricsSubsystem + "_app_build_info"
	require.Equal(t, 1.0, metricByLabels(t, reg, name, map[string]string{"paired_with": "partner"}).GetGauge().GetValue())

	SetAppBuildPairedWith("partner")
	SetAppBuildPairedWith("relay")

	require.Equal(t, 1.0, metricByLabels(t, reg, name, map[string]string{"paired_with": "relay"}).GetGauge().GetValue())
	family := metricFamilyByName(t, reg, name)
	require.Len(t, family.Metric, 1)
	for _, label := range family.Metric[0].Label {
		if label.GetName() == "paired_with" {
			require.Equal(t, "relay", label.GetValue())
		}
	}
}
