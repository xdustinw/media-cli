package media

import "strconv"

// HumanSize formats a byte count as e.g. "947KB", "12MB", "1.4GB".
func HumanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + "B"
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	val := float64(n) / float64(div)
	suffix := []string{"KB", "MB", "GB", "TB", "PB"}[exp]
	if val < 10 {
		return strconv.FormatFloat(val, 'f', 1, 64) + suffix
	}
	return strconv.FormatFloat(val, 'f', 0, 64) + suffix
}
