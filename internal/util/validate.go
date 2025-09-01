package util

import (
	"fmt"
	"strconv"
	"strings"
)

// ValidatePositiveInteger ensures a string is a positive integer
func ValidatePositiveInteger(value string, fieldName string) error {
	if value == "" {
		return fmt.Errorf("%s cannot be empty", fieldName)
	}
	val, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("%s '%s' is invalid: must be a positive integer", fieldName, value)
	}
	if val < 1 {
		return fmt.Errorf("%s %d is invalid: must be a positive integer", fieldName, val)
	}
	return nil
}

// ValidateNonEmpty ensures a string is not empty or whitespace-only
func ValidateNonEmpty(value string, fieldName string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s cannot be empty or contain only whitespace", fieldName)
	}
	return nil
}

// ValidateMinMaxReplicas ensures min <= max and reasonable bounds
func ValidateMinMaxReplicas(minStr, maxStr string) error {
	min, err := strconv.Atoi(minStr)
	if err != nil {
		return fmt.Errorf("minimum replicas '%s' is invalid: must be a positive integer", minStr)
	}

	max, err := strconv.Atoi(maxStr)
	if err != nil {
		return fmt.Errorf("maximum replicas '%s' is invalid: must be a positive integer", maxStr)
	}

	if min > max {
		return fmt.Errorf("minimum replicas (%d) cannot be greater than maximum replicas (%d)", min, max)
	}
	if min < 1 {
		return fmt.Errorf("minimum replicas must be at least 1")
	}
	if max > 100 {
		return fmt.Errorf("maximum replicas cannot exceed 100")
	}
	return nil
}

// ValidateResourceString performs basic validation for CPU/memory/storage strings
func ValidateResourceString(value, fieldName string) error {
	if value == "" {
		return nil
	}
	switch fieldName {
	case "CPU":
		v := strings.TrimSpace(value)
		// Allow millicores like 500m (Kubernetes convention)
		if strings.HasSuffix(v, "m") {
			num := strings.TrimSuffix(v, "m")
			if num == "" {
				return fmt.Errorf("CPU '%s' is invalid: missing number before 'm' (e.g., 500m)", value)
			}
			n, err := strconv.Atoi(num)
			if err != nil || n < 0 {
				return fmt.Errorf("CPU '%s' is invalid: 'm' suffix requires a non-negative integer (e.g., 500m)", value)
			}
			return nil
		}
		// Otherwise require a valid (non-negative) number like 1, 2.5
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f < 0 {
			return fmt.Errorf("CPU '%s' is invalid: must be a number (e.g., 500m, 1, 2.5)", value)
		}
		return nil
	case "Memory", "EphemeralStorage":
		if !strings.HasSuffix(value, "Ki") && !strings.HasSuffix(value, "Mi") &&
			!strings.HasSuffix(value, "Gi") && !strings.HasSuffix(value, "Ti") {
			return fmt.Errorf("%s '%s' is invalid: must include unit (e.g., 512Mi, 1Gi)", fieldName, value)
		}
	}
	return nil
}
