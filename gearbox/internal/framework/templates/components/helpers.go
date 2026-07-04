package components

import (
	"fmt"
	"strings"
)

// formatNumber formats a number with comma separators for thousands
func formatNumber(value float64, decimals int) string {
	// Format with decimals
	formatted := fmt.Sprintf("%.*f", decimals, value)

	// For simplicity, if value is less than 1000, return as-is
	if value < 1000 {
		return formatted
	}

	// Find decimal point
	decimalIndex := strings.Index(formatted, ".")
	intPart := formatted
	decPart := ""

	if decimalIndex > 0 {
		intPart = formatted[:decimalIndex]
		decPart = formatted[decimalIndex:]
	}

	// Add commas to integer part from right to left
	var result strings.Builder
	for i, digit := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			result.WriteRune(',')
		}
		result.WriteRune(digit)
	}

	result.WriteString(decPart)
	return result.String()
}

// calculateCPUPercentage converts CPU load average to a percentage (assuming 4 cores)
func calculateCPUPercentage(loadAvg float64) float64 {
	// Assume 4 cores for percentage calculation
	// Load of 4.0 = 100% utilization
	const numCores = 4.0
	percentage := (loadAvg / numCores) * 100
	if percentage > 100 {
		return 100
	}
	return percentage
}

