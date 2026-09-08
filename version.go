package main

// appVersion is the fallback version for local builds. Release builds override
// it with -ldflags "-X main.appVersion=<version>".
var appVersion = "0.2.0"
