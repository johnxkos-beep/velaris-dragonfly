package worldgen

import "math"

func hash2(x, z int64, seed int64) float64 {
	h := x*374761393 + z*668265263 + seed*2246822519
	h = (h ^ (h >> 13)) * 1274126177
	h = h ^ (h >> 16)
	v := uint64(h) & 0x7FFFFFFFFFFFFFFF
	return float64(v%1000000) / 1000000.0
}

func hash3(x, y, z int64, seed int64) float64 {
	h := x*374761393 + y*668265263 + z*2147483647 + seed*2246822519
	h = (h ^ (h >> 13)) * 1274126177
	h = h ^ (h >> 16)
	v := uint64(h) & 0x7FFFFFFFFFFFFFFF
	return float64(v%1000000) / 1000000.0
}

func smooth(a, b, t float64) float64 {
	f := (1 - math.Cos(t*math.Pi)) * 0.5
	return a*(1-f) + b*f
}

func valueNoise2D(x, z float64, seed int64) float64 {
	x0, z0 := int64(math.Floor(x)), int64(math.Floor(z))
	tx, tz := x-float64(x0), z-float64(z0)
	i1 := smooth(hash2(x0, z0, seed), hash2(x0+1, z0, seed), tx)
	i2 := smooth(hash2(x0, z0+1, seed), hash2(x0+1, z0+1, seed), tx)
	return smooth(i1, i2, tz)
}

// fractal2D combines several octaves of valueNoise2D for more natural-looking
// terrain than a single noise layer would give.
func fractal2D(x, z float64, seed int64, octaves int, persistence float64) float64 {
	total, amp, max, freq := 0.0, 1.0, 0.0, 1.0
	for i := 0; i < octaves; i++ {
		total += valueNoise2D(x*freq, z*freq, seed+int64(i)*911) * amp
		max += amp
		amp *= persistence
		freq *= 2
	}
	return total / max
}

func valueNoise3D(x, y, z float64, seed int64) float64 {
	x0, y0, z0 := int64(math.Floor(x)), int64(math.Floor(y)), int64(math.Floor(z))
	tx, ty, tz := x-float64(x0), y-float64(y0), z-float64(z0)
	c000, c100 := hash3(x0, y0, z0, seed), hash3(x0+1, y0, z0, seed)
	c010, c110 := hash3(x0, y0+1, z0, seed), hash3(x0+1, y0+1, z0, seed)
	c001, c101 := hash3(x0, y0, z0+1, seed), hash3(x0+1, y0, z0+1, seed)
	c011, c111 := hash3(x0, y0+1, z0+1, seed), hash3(x0+1, y0+1, z0+1, seed)
	ix00, ix10 := smooth(c000, c100, tx), smooth(c010, c110, tx)
	ix01, ix11 := smooth(c001, c101, tx), smooth(c011, c111, tx)
	iy0, iy1 := smooth(ix00, ix10, ty), smooth(ix01, ix11, ty)
	return smooth(iy0, iy1, tz)
}

// fractal3D combines several octaves of valueNoise3D. Used for carving nether
// caverns.
func fractal3D(x, y, z float64, seed int64, octaves int, persistence float64) float64 {
	total, amp, max, freq := 0.0, 1.0, 0.0, 1.0
	for i := 0; i < octaves; i++ {
		total += valueNoise3D(x*freq, y*freq, z*freq, seed+int64(i)*911) * amp
		max += amp
		amp *= persistence
		freq *= 2
	}
	return total / max
}
