// Copyright 2020 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package hetzner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery/refresh"
	"github.com/prometheus/prometheus/discovery/targetgroup"
)

const (
	hetznerRobotLabelPrefix    = hetznerLabelPrefix + "robot_"
	hetznerLabelRobotProduct   = hetznerRobotLabelPrefix + "product"
	hetznerLabelRobotCancelled = hetznerRobotLabelPrefix + "cancelled"
)

// Discovery periodically performs Hetzner Robot requests. It implements
// the Discoverer interface.
type robotDiscovery struct {
	*refresh.Discovery
	client   *http.Client
	port     int
	endpoint string
}

// newRobotDiscovery returns a new robotDiscovery which periodically refreshes its targets.
func newRobotDiscovery(conf *SDConfig, logger log.Logger) (*robotDiscovery, error) {
	d := &robotDiscovery{
		port:     conf.Port,
		endpoint: conf.robotEndpoint,
	}

	rt, err := config_util.NewRoundTripperFromConfig(conf.HTTPClientConfig, "hetzner_sd", false, false)
	if err != nil {
		return nil, err
	}
	d.client = &http.Client{
		Transport: rt,
		Timeout:   time.Duration(conf.RefreshInterval),
	}

	return d, nil
}
func (d *robotDiscovery) refresh(ctx context.Context) ([]*targetgroup.Group, error) {
	resp, err := d.client.Get(d.endpoint + "/server")
	if err != nil {
		return nil, err
	}
	defer func() {
		io.Copy(ioutil.Discard, resp.Body)
		resp.Body.Close()
	}()
	var servers serversList
	err = json.NewDecoder(resp.Body).Decode(&servers)
	if err != nil {
		return nil, err
	}

	targets := make([]model.LabelSet, len(servers))
	for i, server := range servers {
		labels := model.LabelSet{
			hetznerLabelRole:           model.LabelValue(hetznerRoleRobot),
			hetznerLabelServerID:       model.LabelValue(strconv.Itoa(server.Server.ServerNumber)),
			hetznerLabelServerName:     model.LabelValue(server.Server.ServerName),
			hetznerLabelDatacenter:     model.LabelValue(strings.ToLower(server.Server.Dc)),
			hetznerLabelPublicIPv4:     model.LabelValue(server.Server.ServerIP),
			hetznerLabelServerStatus:   model.LabelValue(server.Server.Status),
			hetznerLabelRobotProduct:   model.LabelValue(server.Server.Product),
			hetznerLabelRobotCancelled: model.LabelValue(fmt.Sprintf("%t", server.Server.Canceled)),

			model.AddressLabel: model.LabelValue(net.JoinHostPort(server.Server.ServerIP, strconv.FormatUint(uint64(d.port), 10))),
		}
		for _, subnet := range server.Server.Subnet {
			ip := net.ParseIP(subnet.IP)
			if ip.To4() == nil {
				labels[hetznerLabelPublicIPv6Network] = model.LabelValue(fmt.Sprintf("%s/%s", subnet.IP, subnet.Mask))
				break
			}

		}
		targets[i] = labels
	}
	return []*targetgroup.Group{{Source: "hetzner", Targets: targets}}, nil
}

type serversList []struct {
	Server struct {
		ServerIP     string `json:"server_ip"`
		ServerNumber int    `json:"server_number"`
		ServerName   string `json:"server_name"`
		Dc           string `json:"dc"`
		Status       string `json:"status"`
		Product      string `json:"product"`
		Canceled     bool   `json:"cancelled"`
		Subnet       []struct {
			IP   string `json:"ip"`
			Mask string `json:"mask"`
		} `json:"subnet"`
	} `json:"server"`
}
