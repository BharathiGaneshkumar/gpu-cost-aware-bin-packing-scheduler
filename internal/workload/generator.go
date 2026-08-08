package workload

import (
	"math/rand"
	"strconv"
)

// Job represents one simulated GPU job in a test batch.
type Job struct {
	Name  string
	Units int
}

type sizeClass struct {
	min, max int
	weight   float64
}

// distribution mirrors a plausible real GPU workload mix: mostly small
// jobs, some medium, few large -- not tuned to flatter any one strategy.
var distribution = []sizeClass{
	{min: 1, max: 3, weight: 0.6}, // small
	{min: 4, max: 6, weight: 0.3}, // medium
	{min: 7, max: 9, weight: 0.1}, // large
}

// GenerateBatch produces a reproducible batch of n jobs using the given
// seed. Same seed + same n always produces the identical batch, so
// different scheduling strategies can be compared fairly on identical
// input.
func GenerateBatch(n int, seed int64) []Job {
	rng := rand.New(rand.NewSource(seed))
	jobs := make([]Job, n)
	for i := 0; i < n; i++ {
		class := pickClass(rng)
		size := class.min + rng.Intn(class.max-class.min+1)
		jobs[i] = Job{
			Name:  "workload-job-" + strconv.Itoa(i),
			Units: size,
		}
	}
	return jobs
}

func pickClass(rng *rand.Rand) sizeClass {
	r := rng.Float64()
	cumulative := 0.0
	for _, c := range distribution {
		cumulative += c.weight
		if r < cumulative {
			return c
		}
	}
	return distribution[len(distribution)-1]
}
