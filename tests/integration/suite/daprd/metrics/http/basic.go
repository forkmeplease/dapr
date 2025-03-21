/*
Copyright 2024 The Dapr Authors
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

package http

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dapr/dapr/tests/integration/framework"
	"github.com/dapr/dapr/tests/integration/framework/process/daprd"
	"github.com/dapr/dapr/tests/integration/framework/process/http/app"
	"github.com/dapr/dapr/tests/integration/suite"
)

func init() {
	suite.Register(new(basic))
}

// basic tests daprd metrics for the HTTP server
type basic struct {
	daprd *daprd.Daprd
}

func (b *basic) Setup(t *testing.T) []framework.Option {
	app := app.New(t,
		app.WithHandlerFunc("/hi", func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, "OK")
		}),
	)

	b.daprd = daprd.New(t,
		daprd.WithAppPort(app.Port()),
		daprd.WithAppProtocol("http"),
		daprd.WithAppID("myapp"),
		daprd.WithInMemoryStateStore("mystore"),
	)

	return []framework.Option{
		framework.WithProcesses(app, b.daprd),
	}
}

func (b *basic) Run(t *testing.T, ctx context.Context) {
	b.daprd.WaitUntilRunning(t, ctx)

	t.Run("service invocation", func(t *testing.T) {
		b.daprd.HTTPGet2xx(t, ctx, "/v1.0/invoke/myapp/method/hi")
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			metrics := b.daprd.Metrics(c, ctx).All()
			assert.Equal(c, 1, int(metrics["dapr_http_server_request_count|app_id:myapp|method:GET|path:/v1.0/invoke/myapp/method/hi|status:200"]))
		}, time.Second*3, time.Millisecond*10)
	})

	t.Run("state stores", func(t *testing.T) {
		body := `[{"key":"myvalue", "value":"hello world"}]`
		b.daprd.HTTPPost2xx(t, ctx, "/v1.0/state/mystore", strings.NewReader(body), "content-type", "application/json")

		b.daprd.HTTPGet2xx(t, ctx, "/v1.0/state/mystore/myvalue")

		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			metrics := b.daprd.Metrics(c, ctx).All()
			assert.Equal(c, 1, int(metrics["dapr_http_server_request_count|app_id:myapp|method:POST|path:/v1.0/state/mystore|status:204"]))
			assert.Equal(c, 1, int(metrics["dapr_http_server_request_count|app_id:myapp|method:GET|path:/v1.0/state/mystore|status:200"]))
		}, time.Second*3, time.Millisecond*10)
	})
}
