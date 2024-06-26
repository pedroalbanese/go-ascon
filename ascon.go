// Package ascon implements the ASCON AEAD cipher.
//
// References:
//
//    [ascon]: https://ascon.iaik.tugraz.at
//
package ascon

import (
    "errors"
    "runtime"
    "strconv"
    "crypto/cipher"
    "encoding/binary"

    "github.com/pedroalbanese/go-ascon/internal/subtle"
)

const (
    iv128  uint64 = 0x80400c0600000000 // Ascon-128
    iv128a uint64 = 0x80800c0800000000 // Ascon-128a
)

var errOpen = errors.New("ascon: message authentication failed")

const (
    // BlockSize128a is the size in bytes of an ASCON-128a block.
    BlockSize128a = 16
    // BlockSize128 is the size in bytes of an ASCON-128 block.
    BlockSize128 = 8
    // KeySize is the size in bytes of ASCON-128 and ASCON-128a
    // keys.
    KeySize = 16
    // NonceSize is the size in bytes of ASCON-128 and ASCON-128a
    // nonces.
    NonceSize = 16
    // TagSize is the size in bytes of ASCON-128 and ASCON-128a
    // authenticators.
    TagSize = 16
)

type ascon struct {
    k0, k1 uint64
    iv     uint64
}

var _ cipher.AEAD = (*ascon)(nil)

// New128 creates a 128-bit ASCON-128 AEAD.
//
// ASCON-128 provides lower throughput but increased robustness
// against partial or full state recovery compared to ASCON-128a.
//
// Each unique key can encrypt a maximum 2^68 bytes (i.e., 2^64
// plaintext and associated data blocks). Nonces must never be
// reused with the same key. Violating either of these
// constraints compromises the security of the algorithm.
//
// There are no other constraints on the composition of the
// nonce. For example, the nonce can be a counter.
//
// Refer to ASCON's documentation for more information.
func New128(key []byte) (cipher.AEAD, error) {
    if len(key) != KeySize {
        return nil, errors.New("ascon: bad key length")
    }

    return &ascon{
        k0: binary.BigEndian.Uint64(key[0:]),
        k1: binary.BigEndian.Uint64(key[8:]),
        iv: iv128,
    }, nil
}

// New128a creates a 128-bit ASCON-128a AEAD.
//
// ASCON-128a provides higher throughput but reduced robustness
// against partial or full state recovery compared to ASCON-128.
//
// Each unique key can encrypt a maximum 2^68 bytes (i.e., 2^64
// plaintext and associated data blocks). Nonces must never be
// reused with the same key. Violating either of these
// constraints compromises the security of the algorithm.
//
// There are no other constraints on the composition of the
// nonce. For example, the nonce can be a counter.
//
// Refer to ASCON's documentation for more information.
func New128a(key []byte) (cipher.AEAD, error) {
    if len(key) != KeySize {
        return nil, errors.New("ascon: bad key length")
    }

    return &ascon{
        k0: binary.BigEndian.Uint64(key[0:]),
        k1: binary.BigEndian.Uint64(key[8:]),
        iv: iv128a,
    }, nil
}

func (a *ascon) NonceSize() int {
    return NonceSize
}

func (a *ascon) Overhead() int {
    return TagSize
}

func (a *ascon) Seal(dst, nonce, plaintext, additionalData []byte) []byte {
    if len(nonce) != NonceSize {
        panic("ascon: incorrect nonce length: " + strconv.Itoa(len(nonce)))
    }

    n0 := binary.BigEndian.Uint64(nonce[0:])
    n1 := binary.BigEndian.Uint64(nonce[8:])

    var s state
    s.init(a.iv, a.k0, a.k1, n0, n1)

    if a.iv == iv128a {
        s.additionalData128a(additionalData)
    } else {
        s.additionalData128(additionalData)
    }

    ret, out := subtle.SliceForAppend(dst, len(plaintext)+TagSize)
    if subtle.InexactOverlap(out, plaintext) {
        panic("ascon: invalid buffer overlap")
    }

    if a.iv == iv128a {
        s.encrypt128a(out[:len(plaintext)], plaintext)
    } else {
        s.encrypt128(out[:len(plaintext)], plaintext)
    }

    if a.iv == iv128a {
        s.finalize128a(a.k0, a.k1)
    } else {
        s.finalize128(a.k0, a.k1)
    }

    s.tag(out[len(out)-TagSize:])

    return ret
}

func (a *ascon) Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error) {
    if len(nonce) != NonceSize {
        panic("ascon: incorrect nonce length: " + strconv.Itoa(len(nonce)))
    }

    if len(ciphertext) < TagSize {
        return nil, errOpen
    }

    tag := ciphertext[len(ciphertext)-TagSize:]
    ciphertext = ciphertext[:len(ciphertext)-TagSize]

    n0 := binary.BigEndian.Uint64(nonce[0:])
    n1 := binary.BigEndian.Uint64(nonce[8:])

    var s state
    s.init(a.iv, a.k0, a.k1, n0, n1)

    if a.iv == iv128a {
        s.additionalData128a(additionalData)
    } else {
        s.additionalData128(additionalData)
    }

    ret, out := subtle.SliceForAppend(dst, len(ciphertext))
    if subtle.InexactOverlap(out, ciphertext) {
        panic("ascon: invalid buffer overlap")
    }

    if a.iv == iv128a {
        s.decrypt128a(out, ciphertext)
    } else {
        s.decrypt128(out, ciphertext)
    }

    if a.iv == iv128a {
        s.finalize128a(a.k0, a.k1)
    } else {
        s.finalize128(a.k0, a.k1)
    }

    expectedTag := make([]byte, TagSize)
    s.tag(expectedTag)

    if subtle.ConstantTimeCompare(expectedTag, tag) != 1 {
        for i := range out {
            out[i] = 0
        }

        runtime.KeepAlive(out)
        return nil, errOpen
    }

    return ret, nil
}
