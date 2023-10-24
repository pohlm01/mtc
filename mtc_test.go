package mtc

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"strings"
	"testing"

	"golang.org/x/crypto/sha3"
)

// Check if hex is the hexdump of data ignoring whitespace
func hexEqual(t *testing.T, data []byte, hx string) {
	hx = strings.ReplaceAll(hx, " ", "")
	hx = strings.ReplaceAll(hx, "\n", "")
	exp, err := hex.DecodeString(hx)
	if err != nil {
		panic(err)
	}
	if hx != fmt.Sprintf("%x", data) {
		t.Fatalf(
			"Expected:\n\n%s\n\nGot:\n\n%s",
			hexdump(exp),
			hexdump(data),
		)
	}
}

func hexdump(data []byte) string {
	buf := &bytes.Buffer{}

	for i := 0; i < len(data); i += 32 {
		for j := 0; j < 32; j += 4 {
			if j != 0 {
				fmt.Fprintf(buf, " ")
			}
			for k := 0; k < 4; k++ {
				if i+j+k >= len(data) {
					fmt.Fprintf(buf, "  ")
				} else {
					fmt.Fprintf(buf, "%02x", data[i+j+k])
				}
			}
		}
		fmt.Fprintf(buf, "\n")
	}

	return buf.String()
}

func createEd25519TestTLSSubject() (*TLSSubject, error) {
	var seed [ed25519.SeedSize]byte

	h := sha3.NewShake128()
	h.Write([]byte("MTC Example"))
	h.Read(seed[:])

	privEd := ed25519.NewKeyFromSeed(seed[:])
	pubEd := privEd.Public()
	return NewTLSSubject(tlsEd25519, pubEd)
}

func createTestAssertion(i int, sub Subject) Assertion {
	return Assertion{
		Subject: sub,
		Claims: Claims{
			DNS: []string{fmt.Sprintf("%d.example.com", i)},
		},
	}
}

func createTestCA() *CAParams {
	ret := CAParams{
		IssuerId:           "example",
		PublicKey:          nil,
		StartTime:          0,
		BatchDuration:      1,
		Lifetime:           10,
		ValidityWindowSize: 10,
		HttpServer:         "example.com",
	}
	return &ret
}

func BenchmarkComputeTree(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _, _ = createTestBatch(b, 100000)
	}
}

// Create a test batch. Return the tree and the first few assertions.
func createTestBatch(t testing.TB, batchSize int) (*Batch, *Tree, []Assertion) {
	sub, err := createEd25519TestTLSSubject()
	if err != nil {
		t.Fatal(err)
	}

	var as []Assertion

	buf := &bytes.Buffer{}
	for i := 0; i < batchSize; i++ {
		a := createTestAssertion(i, sub)
		if i < 100 {
			as = append(as, a)
		}
		aa := a.Abridge()
		aBytes, err := aa.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		buf.Write(aBytes)
	}

	batch := Batch{
		CA:     createTestCA(),
		Number: 123,
	}

	tree, err := batch.ComputeTree(bytes.NewBuffer(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}

	return &batch, tree, as
}

func testComputeTree(t testing.TB, batchSize int) {
	batch, tree, as := createTestBatch(t, batchSize)

	root := tree.Root()
	incorrectRoot := make([]byte, hashLen)

	for i := 0; i < batchSize && i < len(as); i++ {
		path, err := tree.AuthenticationPath(uint64(i))
		if err != nil {
			t.Fatal(err)
		}

		aa := as[i].Abridge()

		err = batch.VerifyAuthenticationPath(
			uint64(i),
			path,
			root,
			&aa,
		)
		if err != nil {
			t.Fatalf("%x %v", path, err)
		}

		err = batch.VerifyAuthenticationPath(
			uint64(i),
			path,
			incorrectRoot,
			&aa,
		)
		if err == nil {
			t.Fatal(err)
		}
	}
}

func TestComputeTree(t *testing.T) {
	for i := 0; i < 16; i++ {
		testComputeTree(t, i)
	}
	testComputeTree(t, 1000)
}

func TestDraftExampleAssertion(t *testing.T) {
	subjectEd, err := createEd25519TestTLSSubject()
	if err != nil {
		t.Fatal(err)
	}

	a := Assertion{
		Subject: subjectEd,
		Claims: Claims{
			DNS: []string{"example.com"},
		},
	}
	buf, err := a.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	hexEqual(t, buf,
		`00000024 08070020 c5d2080f a9a489a2 26b58166 dad00be8 120931a7 69c9c6f1
        f8eefafc 38af9065 00130000 000f000d 000b6578 616d706c 652e636f 6d`,
	)
	aa := a.Abridge()
	buf, err = aa.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	hexEqual(t, buf,
		`00000022 0807d8e2 c44fc82e 175e5698 b1c25324 6c9a996f c37bad29 fd59b6aa
     838b0a93 0b000013 0000000f 000d000b 6578616d 706c652e 636f6d`,
	)

	pubRSA := &rsa.PublicKey{
		E: 65537,
		N: new(big.Int),
	}
	pubRSA.N.SetString("26485197110366765357271253867591331256970571032009498983384617039560142240197243256907412235412914703444465375795783445504244629282526232030120785434168199087387970676542989835576382439558968773244992994383698559298575000012883472188225230198123656650604482882057654263320283841973430258705124596536674745535434496164011626531757268551766322671495046527955146152988361666116193767860898174965362168490825630499022739690144857105900731399665607528557902291677905449622032282601884218205580565913297751522172793268119949330235060676628077192257628116484481264748962841611301588059305121994521179744818537527407051544459", 10)
	subjectRSA, err := NewTLSSubject(tlsPSSWithSHA256, pubRSA)
	if err != nil {
		t.Fatal(err)
	}
	a = Assertion{
		Subject: subjectRSA,
		Claims: Claims{
			DNSWildcard: []string{"example.com"},
			IPv4: []net.IP{
				net.ParseIP("192.0.2.37"),
				net.ParseIP("192.0.12.0"),
				net.ParseIP("198.51.100.60"),
				net.ParseIP("203.0.113.0"),
			},
		},
	}
	buf, err = a.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	hexEqual(t, buf,
		`00000112 0804010e 3082010a 02820101 00d1cd9c d613c050 929e6418 14b4957c
        40f30d07 0927f653 bde7054c 06d53a89 36228b70 72fad4db a186c379 7e00300b
        a5b6de8e 7ab3fed4 cb5a537e 7674916a 130a0435 664428a9 7f1983b7 e028b9ab
        f24700de 1d6478c9 ae361176 daa64c2f 89b42ec0 270add68 85323401 35d22724
        c7bd8f65 075b25b8 96a89ab8 2a2b2194 49b029b8 97e130dc dc96fce1 37351f2b
        7a28f1d0 7b710afb 2c796211 d9ba1feb 43d30810 63f19afd b7ba2ab0 e19fd008
        e719491d d10ed235 5d4790f0 3039e3a3 31aa2644 2d656716 ebe710f2 4260599a
        2d082db1 eccfaa8f f51cfb8e 3dfca0eb e1af59c2 f007b35e 02b0582f 50090018
        b78a6b06 c0188ab3 514d60d6 6243e017 8b020301 00010029 0001000f 000d000b
        6578616d 706c652e 636f6d00 02001200 10c00002 25c0000c 00c63364 3ccb0071
        00`,
	)
	aa = a.Abridge()
	buf, err = aa.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	hexEqual(t, buf,
		`00000022 08049a04 087a4d52 033a0a20 04333359 ccf29703 25684c5f a96f1ca1
        35cb2ab1 f2670029 0001000f 000d000b 6578616d 706c652e 636f6d00 02001200
        10c00002 25c0000c 00c63364 3ccb0071 00`,
	)

	// TODO ecdsa
}

func TestClaimsParsing(t *testing.T) {
	for _, tc := range []Claims{
		Claims{
			DNS: []string{"example.com"},
		},
		Claims{
			DNSWildcard: []string{"example.com"},
		},
		Claims{
			IPv4: []net.IP{net.ParseIP("192.0.2.37")},
		},
		Claims{
			IPv6: []net.IP{net.ParseIP("::1")},
		},
		Claims{
			DNS: []string{
				"example.com",
				"b.example.com",
				"b.b.example.com",
				"ba.example.com",
				"z.example.com",
			},
			DNSWildcard: []string{
				"example.com",
				"b.example.com",
				"b.b.example.com",
				"ba.example.com",
				"z.example.com",
			},
			IPv4: []net.IP{
				net.ParseIP("192.0.2.37"),
				net.ParseIP("192.0.12.0"),
				net.ParseIP("198.51.100.60"),
				net.ParseIP("203.0.113.0"),
			},
			IPv6: []net.IP{
				net.ParseIP("::1"),
			},
		},
	} {
		buf, err := tc.MarshalBinary()
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var tc2 Claims
		err = tc2.UnmarshalBinary(buf)
		if err != nil {
			t.Fatalf("Unmarshal: %v %v", buf, err)
		}

		ptc, _ := json.Marshal(tc)
		ptc2, _ := json.Marshal(tc2)

		if !bytes.Equal(ptc, ptc2) {
			t.Fatalf("%v ≠ %v", tc, tc2)
		}
	}
}
