package sim

// Estimate summarizes the distribution of outcomes across multiple simulation runs.
type Estimate struct {
	// N is the number of simulation runs included in this estimate.
	N int `json:"n"`
	// TotalWeight is the sum of the weights of all included runs.
	TotalWeight float64 `json:"totalWeight"`
	// Mean is the expected value of the objective.
	Mean float64 `json:"mean"`
	// StdDev is the standard deviation of the objective.
	StdDev float64 `json:"stdDev"`
	// CI95Low is the lower bound of the 95% confidence interval for the mean.
	CI95Low float64 `json:"ci95Low"`
	// CI95High is the upper bound of the 95% confidence interval for the mean.
	CI95High float64 `json:"ci95High"`
	// P50 is the median of the objective.
	P50 float64 `json:"p50"`
	// P95 is the 95th percentile of the objective.
	P95 float64 `json:"p95"`
	// Min is the minimum objective value observed.
	Min float64 `json:"min"`
	// Max is the maximum objective value observed.
	Max float64 `json:"max"`
	// ConstraintProb maps constraint names to the probability of them being satisfied.
	ConstraintProb map[string]float64 `json:"constraintProb"`
	// Exact is true if this estimate was computed via exact enumeration rather than Monte Carlo.
	Exact bool `json:"exact"`
}
