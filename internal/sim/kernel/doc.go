// Package kernel runs mode A simulation episodes: one case per run, driven
// through the production reactive.Scheduler on a virtual clock, with
// behaviors sampled for simulated nodes and policies making decisions. It
// never reads the wall clock and never performs side effects.
package kernel
