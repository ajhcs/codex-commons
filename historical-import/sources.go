package historicalimport

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxEvidenceSourceBytes = 2 << 20

func VerifySourceFiles(manifest Manifest, root string) []ValidationIssue {
	var issues []ValidationIssue
	add := func(path, code, message string) {
		issues = append(issues, ValidationIssue{Path: path, Code: code, Message: message, Blocker: true})
	}
	rootResolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		add("sources", "source_root_unavailable", "source root cannot be resolved")
		return issues
	}
	rootResolved, err = filepath.Abs(rootResolved)
	if err != nil {
		add("sources", "source_root_unavailable", "source root cannot be made absolute")
		return issues
	}
	type result struct {
		digest string
		err    error
	}
	cache := map[string]result{}
	for i, source := range manifest.Sources {
		path := fmt.Sprintf("sources[%d]", i)
		clean := filepath.Clean(source.Locator)
		if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) ||
			clean == ".." || windowsPathPattern.MatchString(clean) {
			add(path+".locator", "unsafe_source_locator", "source locator must stay relative to the review root")
			continue
		}
		if prior, ok := cache[clean]; ok {
			if prior.err != nil {
				add(path+".locator", "source_unreadable", "source file could not be verified")
			} else if prior.digest != source.Digest {
				add(path+".digest", "source_digest_mismatch", "declared digest does not match the source file")
			}
			continue
		}
		targetResolved, resolveErr := filepath.EvalSymlinks(filepath.Join(rootResolved, clean))
		if resolveErr != nil {
			cache[clean] = result{err: resolveErr}
			add(path+".locator", "source_unreadable", "source file could not be verified")
			continue
		}
		targetResolved, resolveErr = filepath.Abs(targetResolved)
		if resolveErr != nil {
			cache[clean] = result{err: resolveErr}
			add(path+".locator", "source_unreadable", "source file could not be verified")
			continue
		}
		relative, relativeErr := filepath.Rel(rootResolved, targetResolved)
		if relativeErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			cache[clean] = result{err: fmt.Errorf("source escaped root")}
			add(path+".locator", "unsafe_source_locator", "resolved source leaves the review root")
			continue
		}
		file, openErr := os.Open(targetResolved)
		if openErr != nil {
			cache[clean] = result{err: openErr}
			add(path+".locator", "source_unreadable", "source file could not be verified")
			continue
		}
		info, statErr := file.Stat()
		if statErr != nil || !info.Mode().IsRegular() || info.Size() > maxEvidenceSourceBytes {
			_ = file.Close()
			cache[clean] = result{err: fmt.Errorf("source is not a bounded regular file")}
			add(path+".locator", "source_unreadable", "source must be a regular file no larger than 2 MiB")
			continue
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, io.LimitReader(file, maxEvidenceSourceBytes+1))
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil {
			cache[clean] = result{err: fmt.Errorf("read source")}
			add(path+".locator", "source_unreadable", "source file could not be verified")
			continue
		}
		digest := "sha256:" + hex.EncodeToString(hash.Sum(nil))
		cache[clean] = result{digest: digest}
		if digest != source.Digest {
			add(path+".digest", "source_digest_mismatch", "declared digest does not match the source file")
		}
	}
	return issues
}
