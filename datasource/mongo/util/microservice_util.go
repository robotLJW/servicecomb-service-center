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

package util

import (
	"context"
	"errors"

	"github.com/go-chassis/cari/discovery"
	"go.mongodb.org/mongo-driver/bson"

	"github.com/apache/servicecomb-service-center/datasource"
	"github.com/apache/servicecomb-service-center/datasource/mongo/client"
	"github.com/apache/servicecomb-service-center/datasource/mongo/db"
	"github.com/apache/servicecomb-service-center/datasource/mongo/sd"
	"github.com/apache/servicecomb-service-center/pkg/util"
)

func GetServiceByID(ctx context.Context, serviceID string) (*db.Service, error) {
	cacheService, ok := sd.Store().Service().Cache().Get(serviceID).(db.Service)
	if !ok {
		//no service in cache,get it from mongodb
		return GetService(ctx, GeneratorServiceFilter(ctx, serviceID))
	}
	return cacheToService(cacheService), nil
}

func GeneratorServiceFilter(ctx context.Context, serviceID string) bson.M {
	domain := util.ParseDomain(ctx)
	project := util.ParseProject(ctx)

	return bson.M{
		db.ColumnDomain:  domain,
		db.ColumnProject: project,
		StringBuilder([]string{db.ColumnService, db.ColumnServiceID}): serviceID}
}

func cacheToService(service db.Service) *db.Service {
	return &db.Service{
		Domain:  service.Domain,
		Project: service.Project,
		Tags:    service.Tags,
		Service: service.Service,
	}
}

func GeneratorServiceNameFilter(ctx context.Context, service *discovery.MicroServiceKey) bson.M {
	domain := util.ParseDomain(ctx)
	project := util.ParseProject(ctx)

	return bson.M{
		db.ColumnDomain:  domain,
		db.ColumnProject: project,
		StringBuilder([]string{db.ColumnService, db.ColumnEnv}):         service.Environment,
		StringBuilder([]string{db.ColumnService, db.ColumnAppID}):       service.AppId,
		StringBuilder([]string{db.ColumnService, db.ColumnServiceName}): service.ServiceName,
		StringBuilder([]string{db.ColumnService, db.ColumnVersion}):     service.Version}
}

func GeneratorServiceAliasFilter(ctx context.Context, service *discovery.MicroServiceKey) bson.M {
	domain := util.ParseDomain(ctx)
	project := util.ParseProject(ctx)

	return bson.M{
		db.ColumnDomain:  domain,
		db.ColumnProject: project,
		StringBuilder([]string{db.ColumnService, db.ColumnEnv}):     service.Environment,
		StringBuilder([]string{db.ColumnService, db.ColumnAppID}):   service.AppId,
		StringBuilder([]string{db.ColumnService, db.ColumnAlias}):   service.Alias,
		StringBuilder([]string{db.ColumnService, db.ColumnVersion}): service.Version}
}

func GetServiceID(ctx context.Context, key *discovery.MicroServiceKey) (string, error) {
	id, err := getServiceID(ctx, GeneratorServiceNameFilter(ctx, key))
	if err != nil && !errors.Is(err, datasource.ErrNoData) {
		return "", err
	}
	if len(id) == 0 && len(key.Alias) != 0 {
		return getServiceID(ctx, GeneratorServiceAliasFilter(ctx, key))
	}
	return id, nil
}

func getServiceID(ctx context.Context, filter bson.M) (serviceID string, err error) {
	svc, err := GetService(ctx, filter)
	if err != nil {
		return
	}
	if svc != nil {
		serviceID = svc.Service.ServiceId
		return
	}
	return
}


func GetService(ctx context.Context, filter bson.M) (*db.Service, error) {
	findRes, err := client.GetMongoClient().FindOne(ctx, db.CollectionService, filter)
	if err != nil {
		return nil, err
	}
	var svc *db.Service
	if findRes.Err() != nil {
		//not get any service,not db err
		return nil, datasource.ErrNoData
	}
	err = findRes.Decode(&svc)
	if err != nil {
		return nil, err
	}
	return svc, nil
}

func GetServices(ctx context.Context, filter bson.M) ([]*discovery.MicroService, error) {
	res, err := client.GetMongoClient().Find(ctx, db.CollectionService, filter)
	if err != nil {
		return nil, err
	}
	var services []*discovery.MicroService
	for res.Next(ctx) {
		var tmp db.Service
		err := res.Decode(&tmp)
		if err != nil {
			return nil, err
		}
		services = append(services, tmp.Service)
	}
	return services, nil
}
