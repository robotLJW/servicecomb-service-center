/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package event

import (
	"fmt"
	"github.com/apache/servicecomb-service-center/datasource/mongo/db"
	"github.com/apache/servicecomb-service-center/datasource/mongo/sd"
	"github.com/apache/servicecomb-service-center/pkg/log"
	"github.com/apache/servicecomb-service-center/server/metrics"
	"github.com/go-chassis/cari/discovery"
)

// ServiceEventHandler is the handler to handle:
// 1. report service metrics
// 2. save the new domain & project mapping
// 3. reset the find instance cache
type ServiceEventHandler struct {
}

func (h *ServiceEventHandler) Type() string {
	return db.CollectionService
}

func (h *ServiceEventHandler) OnEvent(evt sd.MongoEvent) {
	sevice := evt.Value.(db.Service)
	action := evt.Type
	log.Info(fmt.Sprintf("%s", action))
	fn, fv := getFramework(sevice.Service)
	switch action {
	case discovery.EVT_INIT:
		// newDomainProject
		metrics.ReportServices(sevice.Domain, fn, fv, 1)
		return
	case discovery.EVT_CREATE:
		// newDomainProject
		metrics.ReportServices(sevice.Domain, fn, fv, 1)
	case discovery.EVT_DELETE:
		metrics.ReportServices(sevice.Domain, fn, fv, -1)
	default:
	}
	log.Infof("caught [%s] service[%s][%s/%s/%s/%s] event",
		evt.Type, sevice.Service.ServiceId, sevice.Service.Environment, sevice.Service.AppId, sevice.Service.ServiceName, sevice.Service.Version)

	// remove from cache
}

func getFramework(ms *discovery.MicroService) (string, string) {
	if ms.Framework != nil && len(ms.Framework.Name) > 0 {
		version := ms.Framework.Version
		if len(ms.Framework.Version) == 0 {
			version = "UNKNOWN"
		}
		return ms.Framework.Name, version
	}
	return "UNKNOWN", "UNKNOWN"
}

func NewServiceEventHandler() *ServiceEventHandler {
	return &ServiceEventHandler{}
}
