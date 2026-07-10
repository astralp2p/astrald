// Package mobile is the gomobile-bind entry point for the astrald daemon.
//
// It exposes a small, primitive-typed surface so that Java/Kotlin (Android)
// or Objective-C/Swift (iOS) can drive a single in-process node:
//
//   - construct a Node (NewNode)
//   - configure its on-disk roots (SetConfigDir, SetDataDir)
//   - optionally inject a platform Host for OS-level hooks (SetHost)
//   - start the node (Start), stop it (Stop), block until done (Join)
//   - serve queries at the node identity from the wrapper (AddQueryHandler),
//     so platform-level protocols (e.g. a music player) appear as native
//     node services
//   - read object data from the node's repositories (OpenObject), e.g. to
//     stream media to a platform player
//
// Only one Node may be running per process. core/run.go installs a global
// default router during Run, so concurrent Nodes would clobber each other.
//
// To produce an AAR for Android:
//
//	gomobile bind -target=android -o astrald.aar
//	    github.com/astralp2p/astrald/mobile
//
// All public types and methods in this package are bindable by gomobile:
// only string, int, bool, []byte, error, interfaces with bindable methods,
// and opaque struct handles cross the JNI boundary.
package mobile
