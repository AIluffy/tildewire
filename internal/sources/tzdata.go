//go:build timetzdata

package sources

// Import IANA zone data only for builds that opt into self-contained timezone data.
import _ "time/tzdata"
