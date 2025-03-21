/*
Copyright 2021 The Dapr Authors
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

package channel

import (
	"context"
	"crypto/tls"

	"google.golang.org/protobuf/types/known/anypb"

	"github.com/dapr/dapr/pkg/apphealth"
	"github.com/dapr/dapr/pkg/config"
	invokev1 "github.com/dapr/dapr/pkg/messaging/v1"
)

const (
	// AppChannelMinTLSVersion is the minimum TLS version that the app channel will use.
	AppChannelMinTLSVersion = tls.VersionTLS12
)

// AppChannel is an abstraction over communications with user code.
type AppChannel interface {
	GetAppConfig(ctx context.Context, appID string) (*config.ApplicationConfig, error)
	InvokeMethod(ctx context.Context, req *invokev1.InvokeMethodRequest, appID string) (*invokev1.InvokeMethodResponse, error)
	HealthProbe(ctx context.Context) (*apphealth.Status, error)
	SetAppHealth(ah *apphealth.AppHealth)
	TriggerJob(ctx context.Context, name string, data *anypb.Any) (*invokev1.InvokeMethodResponse, error)
}

// HTTPEndpointAppChannel is an abstraction over communications with http endpoint resources.
type HTTPEndpointAppChannel interface {
	InvokeMethod(ctx context.Context, req *invokev1.InvokeMethodRequest, appID string) (*invokev1.InvokeMethodResponse, error)
}
