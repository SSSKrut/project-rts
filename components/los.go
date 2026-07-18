package components

// VisRun is one visible interval [T0, T1) metres along an azimuth ray of the
// LOS-preview fan. Produced by systems.LOSProbe, consumed by 3D and map
// overlays.
type VisRun struct{ T0, T1 float32 }
