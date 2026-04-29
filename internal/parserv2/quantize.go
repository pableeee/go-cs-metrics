package parserv2

import "math"

// quantPos rounds a Hammer-unit float to the nearest int16. CS2 maps fit
// in roughly ±16,384 units so int16 (±32,768) is lossless at integer-unit
// precision; sub-unit precision is below the engine's own resolution and
// not worth preserving.
func quantPos(f float64) int16 {
	if f > math.MaxInt16 {
		return math.MaxInt16
	}
	if f < math.MinInt16 {
		return math.MinInt16
	}
	return int16(math.Round(f))
}

// quantAngle quantises a degree value (any range, including 0–360 yaw) to
// signed 1/100° in int16. Yaws coming from demoinfocs are in [0, 360); we
// fold to [-180, 180] before scaling so the full rotation fits in int16
// (±327.67° headroom).
func quantAngle(degF float64) int16 {
	for degF > 180 {
		degF -= 360
	}
	for degF < -180 {
		degF += 360
	}
	scaled := degF * 100
	if scaled > math.MaxInt16 {
		return math.MaxInt16
	}
	if scaled < math.MinInt16 {
		return math.MinInt16
	}
	return int16(math.Round(scaled))
}

// quantVel rounds a velocity component (Hammer u/s) to int16. Player max
// speed is ~260 u/s and grenade speeds peak around 2200 u/s — both fit
// comfortably in int16 (±32,768).
func quantVel(f float64) int16 {
	if f > math.MaxInt16 {
		return math.MaxInt16
	}
	if f < math.MinInt16 {
		return math.MinInt16
	}
	return int16(math.Round(f))
}

// normalisePitch shifts a CS2 pitch reading from the [0, 360) range
// (where 270° means "looking down 90°") into the more conventional
// [-180, 180] range (negative = looking up, positive = looking down).
func normalisePitch(deg float64) float64 {
	if deg > 180 {
		deg -= 360
	}
	return deg
}
