/*
Copyright 2023 The Dapr Authors
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package security

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapr/dapr/pkg/healthz"
	"github.com/dapr/kit/crypto/spiffe"
)

func Test_Start(t *testing.T) {
	t.Run("trust bundle should be updated when it is changed on file", func(t *testing.T) {
		genRootCA := func() ([]byte, *x509.Certificate) {
			pk, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			require.NoError(t, err)

			serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
			serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
			require.NoError(t, err)
			tmpl := x509.Certificate{
				SerialNumber:          serialNumber,
				NotBefore:             time.Now(),
				NotAfter:              time.Now().Add(time.Minute),
				KeyUsage:              x509.KeyUsageDigitalSignature,
				SignatureAlgorithm:    x509.ECDSAWithSHA256,
				ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
				BasicConstraintsValid: true,
				IsCA:                  true,
			}

			certDER, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &pk.PublicKey, pk)
			require.NoError(t, err)

			wrkloadPK, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			require.NoError(t, err)

			serialNumber, err = rand.Int(rand.Reader, serialNumberLimit)
			require.NoError(t, err)

			spiffeID := spiffeid.RequireFromPath(spiffeid.RequireTrustDomainFromString("test.example.com"), "/ns/foo/bar")

			tmpl = x509.Certificate{
				SerialNumber:          serialNumber,
				NotBefore:             time.Now(),
				NotAfter:              time.Now().Add(time.Minute),
				KeyUsage:              x509.KeyUsageDigitalSignature,
				SignatureAlgorithm:    x509.ECDSAWithSHA256,
				ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
				URIs:                  []*url.URL{spiffeID.URL()},
				BasicConstraintsValid: true,
			}

			workloadCertDER, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &wrkloadPK.PublicKey, pk)
			require.NoError(t, err)

			workloadCert, err := x509.ParseCertificate(workloadCertDER)
			require.NoError(t, err)

			return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), workloadCert
		}

		root1, workloadCert := genRootCA()
		root2, _ := genRootCA()
		tdFile := filepath.Join(t.TempDir(), "root.pem")
		require.NoError(t, os.WriteFile(tdFile, root1, 0o600))

		p, err := New(t.Context(), Options{
			TrustAnchorsFile:        &tdFile,
			AppID:                   "test",
			ControlPlaneTrustDomain: "test.example.com",
			ControlPlaneNamespace:   "default",
			MTLSEnabled:             true,
			OverrideCertRequestFn: func(context.Context, []byte) (*spiffe.SVIDResponse, error) {
				return &spiffe.SVIDResponse{
					X509Certificates: []*x509.Certificate{workloadCert},
				}, nil
			},
			Healthz: healthz.New(),
		})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())

		providerStopped := make(chan struct{})
		go func() {
			defer close(providerStopped)
			assert.NoError(t, p.Run(ctx))
		}()

		prov := p.(*provider)

		select {
		case <-prov.readyCh:
		case <-time.After(time.Second):
			require.FailNow(t, "provider is not ready")
		}

		sec, err := p.Handler(ctx)
		require.NoError(t, err)

		td, err := sec.CurrentTrustAnchors(ctx)
		require.NoError(t, err)
		assert.Equal(t, root1, td)

		caBundleCh := make(chan []byte, 2)
		watcherStopped := make(chan struct{})
		go func() {
			defer close(watcherStopped)
			sec.WatchTrustAnchors(ctx, caBundleCh)
		}()

		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			curr, err := prov.sec.trustAnchors.CurrentTrustAnchors(ctx)
			assert.NoError(c, err)
			assert.Equal(c, root1, curr)
		}, time.Second, time.Millisecond)

		assert.Eventually(t, func() bool {
			// We put the write file inside this assert loop since we have to wait
			// for the fsnotify go rountine to warm up.
			assert.NoError(t, os.WriteFile(tdFile, root2, 0o600))

			curr, err := prov.sec.trustAnchors.CurrentTrustAnchors(ctx)
			assert.NoError(t, err)
			return bytes.Equal(root2, curr)
		}, time.Second*5, time.Millisecond*750)

		t.Run("should expect that the trust bundle watch is updated", func(t *testing.T) {
			select {
			case got := <-caBundleCh:
				assert.Equal(t, root2, got)
			case <-time.After(time.Second * 3):
				require.FailNow(t, "trust bundle watch is not updated in time")
			}
		})

		cancel()

		select {
		case <-providerStopped:
		case <-time.After(time.Second):
			require.FailNow(t, "provider is not stopped")
		}
	})
}

func TestCurrentNamespace(t *testing.T) {
	t.Run("error is namespace is not set", func(t *testing.T) {
		osns, ok := os.LookupEnv("NAMESPACE")
		os.Unsetenv("NAMESPACE")
		t.Cleanup(func() {
			if ok {
				t.Setenv("NAMESPACE", osns)
			}
		})
		ns, err := CurrentNamespaceOrError()
		require.Error(t, err)
		assert.Empty(t, ns)
	})

	t.Run("error if namespace is set but empty", func(t *testing.T) {
		t.Setenv("NAMESPACE", "")
		ns, err := CurrentNamespaceOrError()
		require.Error(t, err)
		assert.Empty(t, ns)
	})

	t.Run("returns namespace if set", func(t *testing.T) {
		t.Setenv("NAMESPACE", "foo")
		ns, err := CurrentNamespaceOrError()
		require.NoError(t, err)
		assert.Equal(t, "foo", ns)
	})
}

func Test_isControlPlaneService(t *testing.T) {
	tests := map[string]struct {
		name string
		exp  bool
	}{
		"operator should be control plane service": {
			name: "dapr-operator",
			exp:  true,
		},
		"sentry should be control plane service": {
			name: "dapr-sentry",
			exp:  true,
		},
		"placement should be control plane service": {
			name: "dapr-placement",
			exp:  true,
		},
		"sidecar injector should be control plane service": {
			name: "dapr-injector",
			exp:  true,
		},
		"not a control plane service": {
			name: "my-app",
			exp:  false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, test.exp, isControlPlaneService(test.name))
		})
	}
}
