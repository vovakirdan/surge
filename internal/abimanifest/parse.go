package abimanifest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Parse strictly decodes and validates a manifest. Duplicate object keys,
// unknown fields, and trailing JSON are rejected rather than ignored.
func Parse(data []byte) (Manifest, error) {
	var manifest Manifest
	if err := rejectDuplicateKeys(data); err != nil {
		return manifest, fmt.Errorf("typed-carrier ABI manifest: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, fmt.Errorf("typed-carrier ABI manifest: decode: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return manifest, fmt.Errorf("typed-carrier ABI manifest: %w", err)
	}
	if err := Validate(&manifest); err != nil {
		return manifest, fmt.Errorf("typed-carrier ABI manifest: %w", err)
	}
	return manifest, nil
}

// LoadCanonical parses a manifest and additionally requires the repository
// representation to equal its deterministic canonical encoding byte-for-byte.
func LoadCanonical(path string) (Manifest, []byte, error) {
	// #nosec G304 -- path is the fixed manifest path beneath the selected repository root.
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("read typed-carrier ABI manifest: %w", err)
	}
	manifest, err := Parse(data)
	if err != nil {
		return Manifest{}, nil, err
	}
	canonical, err := CanonicalBytes(&manifest)
	if err != nil {
		return Manifest{}, nil, err
	}
	if !bytes.Equal(data, canonical) {
		return Manifest{}, nil, fmt.Errorf("typed-carrier ABI manifest is not canonical; run abi-manifest-gen")
	}
	return manifest, canonical, nil
}

// CanonicalBytes returns the sole byte representation accepted for hashing.
func CanonicalBytes(manifest *Manifest) ([]byte, error) {
	if err := Validate(manifest); err != nil {
		return nil, fmt.Errorf("typed-carrier ABI manifest: %w", err)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode typed-carrier ABI manifest: %w", err)
	}
	return append(data, '\n'), nil
}

// ContentHash returns the lowercase SHA-256 identity of canonical content.
func ContentHash(canonical []byte) string {
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

func rejectDuplicateKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := scanJSONValue(decoder, "$", true); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

func scanJSONValue(decoder *json.Decoder, path string, root bool) error {
	token, err := decoder.Token()
	if err != nil {
		if root && err == io.EOF {
			return fmt.Errorf("empty input")
		}
		return fmt.Errorf("decode %s: %w", path, err)
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return fmt.Errorf("decode object key at %s: %w", path, keyErr)
			}
			key, keyOK := keyToken.(string)
			if !keyOK {
				return fmt.Errorf("non-string object key at %s", path)
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate key %q at %s", key, path)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder, path+"."+key, false); err != nil {
				return err
			}
		}
		end, endErr := decoder.Token()
		if endErr != nil || end != json.Delim('}') {
			return fmt.Errorf("unterminated object at %s", path)
		}
	case '[':
		for index := 0; decoder.More(); index++ {
			if err := scanJSONValue(decoder, fmt.Sprintf("%s[%d]", path, index), false); err != nil {
				return err
			}
		}
		end, endErr := decoder.Token()
		if endErr != nil || end != json.Delim(']') {
			return fmt.Errorf("unterminated array at %s", path)
		}
	default:
		return fmt.Errorf("unexpected delimiter %q at %s", delim, path)
	}
	return nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailer any
	if err := decoder.Decode(&trailer); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailer: %w", err)
	}
	return fmt.Errorf("trailing JSON value")
}
