package crypto

import (
	"crypto/rand"
)

// Padding provides padding utilities for traffic obfuscation

// AddPadding pads data to the nearest bucket size
func AddPadding(data []byte, buckets []int) []byte {
	currentLen := len(data)

	// Find the smallest bucket that fits
	for _, bucket := range buckets {
		if bucket >= currentLen+2 { // +2 for length field
			paddingLen := bucket - currentLen - 2
			if paddingLen <= 0 {
				return data
			}
			result := make([]byte, currentLen+paddingLen+2)
			copy(result[:currentLen], data)
			// Random padding bytes
			rand.Read(result[currentLen : currentLen+paddingLen])
			// Store padding length as last 2 bytes (big-endian)
			result[currentLen+paddingLen] = byte(paddingLen >> 8)
			result[currentLen+paddingLen+1] = byte(paddingLen)
			return result
		}
	}

	// No bucket fits, return as-is (no padding)
	return data
}

// StripPadding removes padding from data
func StripPadding(data []byte) ([]byte, error) {
	if len(data) < 2 {
		return data, nil
	}

	// Read padding length from last 2 bytes
	paddingLen := int(data[len(data)-2])<<8 | int(data[len(data)-1])

	if paddingLen+2 > len(data) {
		return data, nil // no valid padding
	}

	return data[:len(data)-paddingLen-2], nil
}
