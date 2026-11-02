package buildinfo

// Product metadata. Version values may be overridden at build time with -ldflags.
const (
ProductName  = "FastV"
ProductID    = "com.mardukprime.fastv"
WailsVersion = "3.1.0"
)

var (
Version   = "0.1.0"
Commit    = "dev"
BuildDate = "unknown"
)
