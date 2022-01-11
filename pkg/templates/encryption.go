// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"github.com/golang/glog"
)

// protect encrypts the input value using AES-CBC. If a salt is set on t.config.Salt, it will prefix the plaintext
// value before it is encrypted. The returned value is in the format of `$ocm_encrypted:<base64 of encrypted string>`.
// An error is returned if the AES key is invalid.
func (t *TemplateResolver) protect(value string) (string, error) {
	if value == "" {
		return value, nil
	}

	block, err := aes.NewCipher(t.config.AESKey)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidAESKey, err)
	}

	// This is already validated in the NewResolver method, but is checked again in case that method was bypassed
	// to avoid a panic.
	if len(t.config.InitializationVector) != IVSize {
		return "", ErrInvalidIV
	}

	blockSize := block.BlockSize()
	blockMode := cipher.NewCBCEncrypter(block, t.config.InitializationVector)

	valueBytes := []byte(value)
	valueBytes = pkcs7Pad(valueBytes, blockSize)

	encryptedValue := make([]byte, len(valueBytes))
	blockMode.CryptBlocks(encryptedValue, valueBytes)

	return protectedPrefix + base64.StdEncoding.EncodeToString(encryptedValue), nil
}

// decrypt will decrypt a string that was encrypted using the protect method. An error is returned if the base64 or
// the AES key is invalid.
func (t *TemplateResolver) decrypt(value string) (string, error) {
	decodedValue, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", fmt.Errorf("%s: %w: %v", value, ErrInvalidB64OfEncrypted, err)
	}

	block, err := aes.NewCipher(t.config.AESKey)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidAESKey, err)
	}

	// This is already validated in the NewResolver method, but is checked again in case that method was bypassed
	// to avoid a panic.
	if len(t.config.InitializationVector) != IVSize {
		return "", ErrInvalidIV
	}

	blockMode := cipher.NewCBCDecrypter(block, t.config.InitializationVector)

	decryptedValue := make([]byte, len(decodedValue))
	blockMode.CryptBlocks(decryptedValue, decodedValue)

	decryptedValue, err = pkcs7Unpad(decryptedValue)
	if err != nil {
		return "", err
	}

	return string(decryptedValue), nil
}

// pkcs7Pad right-pads the given value to match the input block size for AES encryption. The padding
// ranges from 1 byte to the number of bytes equal to the block size.
// Inspired from https://gist.github.com/huyinghuan/7bf174017bf54efb91ece04a48589b22.
func pkcs7Pad(value []byte, blockSize int) []byte {
	// Determine the amount of padding that is required in order to make the plaintext value
	// divisible by the block size. If it is already divisible by the block size, then the padding
	// amount will be a whole block. This is to ensure there is always padding.
	paddingAmount := blockSize - (len(value) % blockSize)
	// Create a new byte slice that can contain the plaintext value and the padding.
	paddedValue := make([]byte, len(value)+paddingAmount)
	// Copy the original value into the new byte slice.
	copy(paddedValue, value)
	// Add the padding to the new byte slice. Each padding byte is the byte representation of the
	// padding amount. This ensures that the last byte of the padded plaintext value refers to the
	// amount of padding to remove when unpadded.
	copy(paddedValue[len(value):], bytes.Repeat([]byte{byte(paddingAmount)}, paddingAmount))

	return paddedValue
}

// pkcs7Unpad unpads data from the given padded value. The last byte must be the number of bytes of padding to remove.
// The ErrInvalidPKCS7Padding error is returned if the value does not have valid padding. This could happen if the user
// did not use the "protect" method to encrypt the data and provided an invalid value.
// Inspired from https://gist.github.com/huyinghuan/7bf174017bf54efb91ece04a48589b22.
func pkcs7Unpad(paddedValue []byte) ([]byte, error) {
	// Determine the amount of padding bytes to remove by checking the value of the last byte.
	lastByte := paddedValue[len(paddedValue)-1]
	numPaddingBytes := int(lastByte)

	// Verify that the last byte is a valid padding length.
	if numPaddingBytes == 0 || numPaddingBytes > len(paddedValue) {
		return nil, fmt.Errorf("%w: the padding length is invalid", ErrInvalidPKCS7Padding)
	}

	// Verify that the declared padding is valid by checking that the padding is all the same byte.
	// i > 1 is the conditional to avoid checking that the last byte is equal to itself.
	for i := numPaddingBytes; i > 1; i-- {
		if paddedValue[len(paddedValue)-i] != lastByte {
			return nil, fmt.Errorf("%w: not all the padding bytes match", ErrInvalidPKCS7Padding)
		}
	}

	// Remove the padding from the byte slice.
	return paddedValue[:len(paddedValue)-numPaddingBytes], nil
}

// processEncryptedStrs replaces all encrypted strings with the decrypted values. Each decryption is handled
// concurrently and the concurrency limit is controlled by t.config.DecryptionConcurrency. If a decryption fails,
// the rest of the decryption is halted and an error is returned.
func (t *TemplateResolver) processEncryptedStrs(templateStr string) (string, error) {
	// This catching any encrypted string in the format of $ocm_encrypted:<base64 of the encrypted value>.
	re := regexp.MustCompile(regexp.QuoteMeta(protectedPrefix) + "([a-zA-Z0-9+/=]+)")
	// Each submatch will have index 0 be the whole match and index 1 as the base64 of the encrypted value.
	submatches := re.FindAllStringSubmatch(templateStr, -1)

	if len(submatches) == 0 {
		return templateStr, nil
	}

	var numWorkers int

	// Determine how many Goroutines to spawn.
	if t.config.DecryptionConcurrency <= 1 {
		numWorkers = 1
	} else if len(submatches) > int(t.config.DecryptionConcurrency) {
		numWorkers = int(t.config.DecryptionConcurrency)
	} else {
		numWorkers = len(submatches)
	}

	submatchesChan := make(chan []string, len(submatches))
	resultsChan := make(chan decryptResult, len(submatches))

	glog.V(glogDefLvl).Infof("Will decrypt %d value(s) with %d Goroutines", len(submatches), numWorkers)

	// Create a context to be able to cancel decryption in case one fails.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start up all the Goroutines.
	for i := 0; i < numWorkers; i++ {
		go t.decryptWrapper(ctx, submatchesChan, resultsChan)
	}

	// Send all the submatches of all the encrypted strings to the Goroutines to process.
	for _, submatch := range submatches {
		submatchesChan <- submatch
	}

	processed := templateStr
	processedResults := 0

	for result := range resultsChan {
		// If an error occurs, stop the Goroutines and return the error.
		if result.err != nil {
			// Cancel the context so the Goroutines exit before the channels close.
			cancel()
			close(submatchesChan)
			close(resultsChan)
			glog.Errorf("Decryption failed %v", result.err)

			return "", fmt.Errorf("decryption of %s failed: %w", result.match, result.err)
		}

		processed = strings.Replace(processed, result.match, result.plaintext, 1)
		processedResults++

		// Once the decryption is complete, it's safe to close the channels and stop blocking in this Goroutine.
		if processedResults == len(submatches) {
			close(submatchesChan)
			close(resultsChan)
		}
	}

	glog.V(glogDefLvl).Infof("Finished decrypting %d value(s)", len(submatches))

	return processed, nil
}

// decryptResult is the result sent back on the "results" channel in decryptWrapper.
type decryptResult struct {
	match     string
	plaintext string
	err       error
}

// decryptWrapper wraps the decrypt method for concurrency. ctx is the context that will get canceled if one or more
// decryptions fail. This will halt the Goroutine early. submatches is the channel with the incoming strings to decrypt
// which gets closed when all the encrypted values have been decrypted. Its values are string slices with the first
// index being the whole string that will be replaced and second index being the base64 of the encrypted string. results
// is a channel to communicate back to the calling Goroutine.
func (t *TemplateResolver) decryptWrapper(
	ctx context.Context, submatches <-chan []string, results chan<- decryptResult,
) {
	for submatch := range submatches {
		match := submatch[0]
		encryptedValue := submatch[1]
		var result decryptResult

		plaintext, err := t.decrypt(encryptedValue)
		if err != nil {
			result = decryptResult{match, "", err}
		} else {
			result = decryptResult{match, plaintext, nil}
		}

		select {
		case <-ctx.Done():
			// Return when decryption has been canceled.
			return
		case results <- result:
			// Continue on to the next submatch.
			continue
		}
	}
}
