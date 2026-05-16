package githubreadme

import (
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/AIluffy/tildewire/internal/tui/markdown"
)

var (
	markdownImagePattern          = regexp.MustCompile(`!\[[^\]]*\]\([^)]+\)`)
	markdownImageWithPartsPattern = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
	markdownLinkedImagePattern    = regexp.MustCompile(`\[!\[([^\]]*)\]\(([^)]+)\)\]\([^)]+\)`)
	htmlImgTagPattern             = regexp.MustCompile(`(?i)<img\b[^>]*>`)
	htmlAttrPattern               = regexp.MustCompile(`(?i)\b([a-z0-9_-]+)\s*=\s*("([^"]*)"|'([^']*)'|([^\s>]+))`)
	htmlPictureSourcePattern      = regexp.MustCompile(`(?i)<\s*/?\s*(picture|source)\b[^>]*>`)
	htmlImageWrapperOnlyPattern   = regexp.MustCompile(`(?i)^\s*</?\s*(a|p|div|center)\b[^>]*>\s*$`)
	htmlTagPattern                = regexp.MustCompile(`(?i)<\s*/?\s*[a-z][a-z0-9:-]*\b[^>]*>`)
)

// ImageRefs extracts unique non-badge image refs from a GitHub README.
func ImageRefs(markdownText string, readmeURL string) []markdown.ImageRef {
	lines := strings.Split(markdownText, "\n")
	refs := make([]markdown.ImageRef, 0)
	seen := make(map[string]bool)
	inCodeBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if isMarkdownFenceLine(trimmed) {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock {
			continue
		}
		for _, ref := range parseImageLine(trimmed, readmeURL) {
			if seen[ref.URL] {
				continue
			}
			refs = append(refs, ref)
			seen[ref.URL] = true
		}
	}
	return refs
}

// ResolveImageURL resolves a README-relative image URL to a raw GitHub URL.
func ResolveImageURL(src, readmeURL string) (string, bool) {
	src = cleanMarkdownImageDestination(src)
	if src == "" || strings.HasPrefix(src, "#") {
		return "", false
	}
	parsedSrc, err := url.Parse(src)
	if err == nil && parsedSrc.IsAbs() {
		if parsedSrc.Scheme == "http" || parsedSrc.Scheme == "https" {
			return parsedSrc.String(), true
		}
		return "", false
	}
	if strings.HasPrefix(src, "//") {
		return "https:" + src, true
	}
	rawBase, ok := rawBase(readmeURL)
	if !ok {
		return "", false
	}
	srcURL, err := url.Parse(src)
	if err != nil {
		return "", false
	}
	srcPath := srcURL.Path
	baseDir := rawBase.dir
	if strings.HasPrefix(srcPath, "/") {
		baseDir = ""
		srcPath = strings.TrimPrefix(srcPath, "/")
	}
	cleaned := path.Clean(path.Join(baseDir, srcPath))
	if cleaned == "." {
		cleaned = ""
	}
	raw := url.URL{
		Scheme:   "https",
		Host:     "raw.githubusercontent.com",
		Path:     "/" + path.Join(rawBase.owner, rawBase.repo, rawBase.branch, cleaned),
		RawQuery: srcURL.RawQuery,
	}
	return raw.String(), true
}

func parseImageLine(line string, readmeURL string) []markdown.ImageRef {
	rawRefs := parseReadmeImageLine(strings.TrimSpace(line))
	refs := make([]markdown.ImageRef, 0, len(rawRefs))
	seen := make(map[string]bool)
	for _, ref := range rawRefs {
		if isBadgeImageURL(ref.Src) {
			continue
		}
		resolved, ok := ResolveImageURL(ref.Src, readmeURL)
		if !ok || seen[resolved] {
			continue
		}
		ref.URL = resolved
		refs = append(refs, ref)
		seen[resolved] = true
	}
	return refs
}

func parseReadmeImageLine(line string) []markdown.ImageRef {
	switch {
	case imageOnlyMarkdownLine(line):
		return parseMarkdownImageRefs(line)
	case imageOnlyHTMLLine(line):
		return parseHTMLImageRefs(line)
	default:
		return nil
	}
}

func imageOnlyMarkdownLine(line string) bool {
	if !markdownImagePattern.MatchString(line) {
		return false
	}
	withoutLinkedImages := markdownLinkedImagePattern.ReplaceAllString(line, "")
	withoutImages := markdownImagePattern.ReplaceAllString(withoutLinkedImages, "")
	return strings.TrimSpace(withoutImages) == ""
}

func parseMarkdownImageRefs(line string) []markdown.ImageRef {
	matches := markdownImageWithPartsPattern.FindAllStringSubmatch(line, -1)
	refs := make([]markdown.ImageRef, 0, len(matches))
	for _, match := range matches {
		if len(match) != 3 {
			continue
		}
		src := cleanMarkdownImageDestination(match[2])
		if src == "" {
			continue
		}
		refs = append(refs, markdown.ImageRef{
			Alt: imageAltOrDefault(match[1]),
			Src: src,
		})
	}
	return refs
}

func imageOnlyHTMLLine(line string) bool {
	if !htmlImgTagPattern.MatchString(line) {
		return false
	}
	withoutImages := htmlImgTagPattern.ReplaceAllString(line, "")
	withoutTags := htmlTagPattern.ReplaceAllString(withoutImages, "")
	return strings.TrimSpace(withoutTags) == ""
}

func htmlImageBadgeOnlyLine(line string) bool {
	refs := parseHTMLImageRefs(line)
	if len(refs) == 0 {
		return false
	}
	for _, ref := range refs {
		if !isBadgeImageURL(ref.Src) {
			return false
		}
	}
	return true
}

func parseHTMLImageRefs(line string) []markdown.ImageRef {
	tags := htmlImgTagPattern.FindAllString(line, -1)
	refs := make([]markdown.ImageRef, 0, len(tags))
	for _, tag := range tags {
		attrs := htmlAttrs(tag)
		src := cleanMarkdownImageDestination(attrs["src"])
		if src == "" {
			src = firstHTMLSrcSetCandidate(attrs["srcset"])
		}
		if src == "" {
			continue
		}
		refs = append(refs, markdown.ImageRef{
			Alt: imageAltOrDefault(attrs["alt"]),
			Src: src,
		})
	}
	return refs
}

func firstHTMLSrcSetCandidate(srcset string) string {
	for _, candidate := range strings.Split(srcset, ",") {
		fields := strings.Fields(strings.TrimSpace(candidate))
		if len(fields) > 0 {
			return cleanMarkdownImageDestination(fields[0])
		}
	}
	return ""
}

func htmlAttrs(tag string) map[string]string {
	attrs := make(map[string]string)
	for _, match := range htmlAttrPattern.FindAllStringSubmatch(tag, -1) {
		if len(match) != 6 {
			continue
		}
		value := match[3]
		if value == "" {
			value = match[4]
		}
		if value == "" {
			value = match[5]
		}
		attrs[strings.ToLower(match[1])] = value
	}
	return attrs
}

func stripBadgeImagesFromLine(line string) string {
	original := line
	line = markdownLinkedImagePattern.ReplaceAllStringFunc(line, func(match string) string {
		parts := markdownLinkedImagePattern.FindStringSubmatch(match)
		if len(parts) == 3 && isBadgeImageURL(cleanMarkdownImageDestination(parts[2])) {
			return ""
		}
		return match
	})
	line = markdownImagePattern.ReplaceAllStringFunc(line, func(match string) string {
		parts := markdownImageWithPartsPattern.FindStringSubmatch(match)
		if len(parts) == 3 && isBadgeImageURL(cleanMarkdownImageDestination(parts[2])) {
			return ""
		}
		return match
	})
	if line != original {
		return strings.TrimSpace(line)
	}
	return line
}

func cleanMarkdownImageDestination(destination string) string {
	destination = strings.TrimSpace(destination)
	destination = strings.Trim(destination, "<>")
	if strings.ContainsAny(destination, " \t") {
		fields := strings.Fields(destination)
		if len(fields) > 0 {
			destination = fields[0]
		}
	}
	return strings.TrimSpace(destination)
}

func imageAltOrDefault(alt string) string {
	alt = strings.TrimSpace(alt)
	if alt == "" {
		return "image"
	}
	return alt
}

func isBadgeImageURL(src string) bool {
	parsed, err := url.Parse(src)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Host)
	return host == "img.shields.io" || strings.HasSuffix(host, ".shields.io")
}

func isMarkdownFenceLine(trimmed string) bool {
	if len(trimmed) < 3 {
		return false
	}
	fenceChar := trimmed[0]
	if fenceChar != '`' && fenceChar != '~' {
		return false
	}
	end := 0
	for end < len(trimmed) && trimmed[end] == fenceChar {
		end++
	}
	return end >= 3
}

type rawBaseInfo struct {
	owner  string
	repo   string
	branch string
	dir    string
}

func rawBase(readmeURL string) (rawBaseInfo, bool) {
	parsed, err := url.Parse(readmeURL)
	if err != nil {
		return rawBaseInfo{}, false
	}
	parts := splitURLPath(parsed.Path)
	switch strings.ToLower(parsed.Host) {
	case "github.com":
		if len(parts) < 5 || parts[2] != "blob" {
			return rawBaseInfo{}, false
		}
		return rawBaseInfo{
			owner:  parts[0],
			repo:   parts[1],
			branch: parts[3],
			dir:    readmeDir(parts[4:]),
		}, true
	case "raw.githubusercontent.com":
		if len(parts) < 4 {
			return rawBaseInfo{}, false
		}
		if info, ok := rawRefsBase(parts); ok {
			return info, true
		}
		return rawBaseInfo{
			owner:  parts[0],
			repo:   parts[1],
			branch: parts[2],
			dir:    readmeDir(parts[3:]),
		}, true
	default:
		return rawBaseInfo{}, false
	}
}

func rawRefsBase(parts []string) (rawBaseInfo, bool) {
	if len(parts) < 6 || parts[2] != "refs" {
		return rawBaseInfo{}, false
	}
	switch parts[3] {
	case "heads", "tags":
	default:
		return rawBaseInfo{}, false
	}
	return rawBaseInfo{
		owner:  parts[0],
		repo:   parts[1],
		branch: parts[4],
		dir:    readmeDir(parts[5:]),
	}, true
}

func readmeDir(parts []string) string {
	if len(parts) <= 1 {
		return ""
	}
	return path.Dir(path.Join(parts...))
}

func splitURLPath(value string) []string {
	raw := strings.Split(strings.Trim(value, "/"), "/")
	parts := make([]string, 0, len(raw))
	for _, part := range raw {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}
