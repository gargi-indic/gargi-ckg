package sim_test

import (
	"testing"

	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

func TestDistribution_Validate(t *testing.T) {
	tests := []struct {
		name    string
		dist    sim.Distribution
		wantErr bool
	}{
		// Normal
		{
			name:    "normal valid",
			dist:    sim.Distribution{Kind: sim.DistKindNormal, Params: map[string]float64{"mean": 0, "sd": 1}},
			wantErr: false,
		},
		{
			name:    "normal missing mean",
			dist:    sim.Distribution{Kind: sim.DistKindNormal, Params: map[string]float64{"sd": 1}},
			wantErr: true,
		},
		{
			name:    "normal missing sd",
			dist:    sim.Distribution{Kind: sim.DistKindNormal, Params: map[string]float64{"mean": 0}},
			wantErr: true,
		},
		{
			name:    "normal sd <= 0",
			dist:    sim.Distribution{Kind: sim.DistKindNormal, Params: map[string]float64{"mean": 0, "sd": 0}},
			wantErr: true,
		},
		// LogNormal
		{
			name:    "lognormal valid",
			dist:    sim.Distribution{Kind: sim.DistKindLogNormal, Params: map[string]float64{"mu": 0, "sigma": 1}},
			wantErr: false,
		},
		{
			name:    "lognormal missing mu",
			dist:    sim.Distribution{Kind: sim.DistKindLogNormal, Params: map[string]float64{"sigma": 1}},
			wantErr: true,
		},
		{
			name:    "lognormal missing sigma",
			dist:    sim.Distribution{Kind: sim.DistKindLogNormal, Params: map[string]float64{"mu": 0}},
			wantErr: true,
		},
		{
			name:    "lognormal sigma <= 0",
			dist:    sim.Distribution{Kind: sim.DistKindLogNormal, Params: map[string]float64{"mu": 0, "sigma": 0}},
			wantErr: true,
		},
		// Uniform
		{
			name:    "uniform valid",
			dist:    sim.Distribution{Kind: sim.DistKindUniform, Params: map[string]float64{"min": 0, "max": 1}},
			wantErr: false,
		},
		{
			name:    "uniform missing min",
			dist:    sim.Distribution{Kind: sim.DistKindUniform, Params: map[string]float64{"max": 1}},
			wantErr: true,
		},
		{
			name:    "uniform missing max",
			dist:    sim.Distribution{Kind: sim.DistKindUniform, Params: map[string]float64{"min": 0}},
			wantErr: true,
		},
		{
			name:    "uniform min >= max",
			dist:    sim.Distribution{Kind: sim.DistKindUniform, Params: map[string]float64{"min": 1, "max": 0}},
			wantErr: true,
		},
		{
			name:    "uniform min == max",
			dist:    sim.Distribution{Kind: sim.DistKindUniform, Params: map[string]float64{"min": 1, "max": 1}},
			wantErr: true,
		},
		// Triangular
		{
			name:    "triangular valid",
			dist:    sim.Distribution{Kind: sim.DistKindTriangular, Params: map[string]float64{"min": 0, "mode": 1, "max": 2}},
			wantErr: false,
		},
		{
			name:    "triangular valid min==mode",
			dist:    sim.Distribution{Kind: sim.DistKindTriangular, Params: map[string]float64{"min": 1, "mode": 1, "max": 2}},
			wantErr: false,
		},
		{
			name:    "triangular valid mode==max",
			dist:    sim.Distribution{Kind: sim.DistKindTriangular, Params: map[string]float64{"min": 0, "mode": 2, "max": 2}},
			wantErr: false,
		},
		{
			name:    "triangular valid min==mode==max",
			dist:    sim.Distribution{Kind: sim.DistKindTriangular, Params: map[string]float64{"min": 2, "mode": 2, "max": 2}},
			wantErr: false,
		},
		{
			name:    "triangular missing min",
			dist:    sim.Distribution{Kind: sim.DistKindTriangular, Params: map[string]float64{"mode": 1, "max": 2}},
			wantErr: true,
		},
		{
			name:    "triangular missing mode",
			dist:    sim.Distribution{Kind: sim.DistKindTriangular, Params: map[string]float64{"min": 0, "max": 2}},
			wantErr: true,
		},
		{
			name:    "triangular missing max",
			dist:    sim.Distribution{Kind: sim.DistKindTriangular, Params: map[string]float64{"min": 0, "mode": 1}},
			wantErr: true,
		},
		{
			name:    "triangular min > mode",
			dist:    sim.Distribution{Kind: sim.DistKindTriangular, Params: map[string]float64{"min": 2, "mode": 1, "max": 3}},
			wantErr: true,
		},
		{
			name:    "triangular mode > max",
			dist:    sim.Distribution{Kind: sim.DistKindTriangular, Params: map[string]float64{"min": 0, "mode": 3, "max": 2}},
			wantErr: true,
		},
		// Empirical
		{
			name:    "empirical valid",
			dist:    sim.Distribution{Kind: sim.DistKindEmpirical, Values: []float64{1, 2, 3}},
			wantErr: false,
		},
		{
			name:    "empirical empty",
			dist:    sim.Distribution{Kind: sim.DistKindEmpirical, Values: []float64{}},
			wantErr: true,
		},
		{
			name:    "empirical nil",
			dist:    sim.Distribution{Kind: sim.DistKindEmpirical, Values: nil},
			wantErr: true,
		},
		// Unknown Kind
		{
			name:    "unknown kind",
			dist:    sim.Distribution{Kind: "unknown", Params: map[string]float64{"min": 0, "max": 1}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.dist.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Distribution.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
