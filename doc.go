// Package wapp provides the WAPP (Wippy Pack) binary archive format.
//
// Format: Header(268) + data frames + compressed TOC + Footer(16)
//
// Features:
//   - Per-file compression based on content type
//   - Lazy loading with footer-first reading
//   - Multiple filesystem tree resources
//   - Integrity verification via SHA-256
package wapp
