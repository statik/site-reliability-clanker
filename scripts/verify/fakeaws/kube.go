package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The stand-in for the API server of an EKS cluster. It serves TLS with a certificate that
// is its own CA, so a kubeconfig must carry that certificate. It refuses every request,
// and records the token of each one: the presigned STS request that `aws eks get-token`
// makes. The record holds the key ID, the region and the signed headers of the token, and
// no signature and no session token.

// kubeNames are the names of the certificate: a runner container reaches this machine as
// host.docker.internal.
var kubeNames = []string{"host.docker.internal", "localhost"}

// writeKubeCertificate writes cert.pem and key.pem into dir.
func writeKubeCertificate(dir string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(now.UnixNano()),
		Subject: pkix.Name{CommonName: "fakeaws-kubernetes"}, NotBefore: now.Add(-time.Hour),
		NotAfter: now.Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    kubeNames, IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(filepath.Join(dir, "cert.pem"), certPEM, 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "key.pem"), keyPEM, 0o600)
}

// tokenFacts reads the bearer token of a request as `aws eks get-token` makes it:
// k8s-aws-v1. and the presigned URL of sts:GetCallerIdentity in base64url.
func tokenFacts(header string) map[string]any {
	token, found := strings.CutPrefix(header, "Bearer k8s-aws-v1.")
	if !found {
		return map[string]any{"error": "no token of aws eks get-token"}
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return map[string]any{"error": "the token is not base64url: " + err.Error()}
	}
	presigned, err := url.Parse(string(raw))
	if err != nil {
		return map[string]any{"error": "the token is not a URL: " + err.Error()}
	}
	query := presigned.Query()
	// Credential is <key ID>/<date>/<region>/<service>/aws4_request.
	credential := strings.Split(query.Get("X-Amz-Credential"), "/")
	if len(credential) != 5 {
		return map[string]any{"error": "the presigned URL has no credential"}
	}
	return map[string]any{"host": presigned.Host, "action": query.Get("Action"),
		"access_key_id": credential[0], "region": credential[2], "service": credential[3],
		"signed_headers":    query.Get("X-Amz-SignedHeaders"),
		"has_session_token": query.Get("X-Amz-Security-Token") != ""}
}

// kubeHandler records each request and refuses it as the API server refuses a user without
// rights.
func kubeHandler(record *recorder) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record.add("kube", r.Method+" "+r.URL.Path, r,
			map[string]any{"token": tokenFacts(r.Header.Get("Authorization"))})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{"kind": "Status", "apiVersion": "v1",
			"status": "Failure", "reason": "Forbidden", "code": http.StatusForbidden,
			"message": fmt.Sprintf("the stand-in cluster refuses %s %s", r.Method, r.URL.Path)})
	})
}
