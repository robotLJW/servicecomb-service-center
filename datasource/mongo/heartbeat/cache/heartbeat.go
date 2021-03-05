/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except request compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to request writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package heartbeatcache

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/patrickmn/go-cache"
	"go.mongodb.org/mongo-driver/bson"

	"github.com/apache/servicecomb-service-center/datasource/mongo"
	"github.com/apache/servicecomb-service-center/datasource/mongo/client"
	"github.com/apache/servicecomb-service-center/datasource/mongo/db"
	"github.com/apache/servicecomb-service-center/pkg/gopool"
	"github.com/apache/servicecomb-service-center/pkg/log"
	"github.com/apache/servicecomb-service-center/server/config"
)

const (
	defaultTTL              = 30
	defaultCacheCapacity    = 10000
	defaultWorkNum          = 10
	defaultTimeout          = 10
	instanceCheckerInternal = 1 * time.Second
	ctxTimeout              = 5 * time.Second
)

// Store cache structure
type instanceHeartbeatInfo struct {
	serviceID   string
	instanceID  string
	ttl         int32
	lastRefresh time.Time
}

var (
	cacheChan              chan *instanceHeartbeatInfo
	instanceHeartbeatStore = cache.New(0, instanceCheckerInternal)
	workerNum              = runtime.NumCPU()
	heartbeatTaskTimeout   = config.GetInt("registry.mongo.heartbeat.timeout", defaultTimeout)
	ErrHeartbeatTimeout    = errors.New("heartbeat task waiting for processing timeout. ")
)

func init() {
	capacity := config.GetInt("registry.mongo.heartbeat.cacheCapacity", defaultCacheCapacity)
	cacheChan = make(chan *instanceHeartbeatInfo, capacity)
	num := config.GetInt("registry.mongo.heartbeat.workerNum", defaultWorkNum)
	if num != 0 {
		workerNum = num
	}
	instanceHeartbeatStore.OnEvicted(func(k string, v interface{}) {
		instanceInfo, ok := v.(*instanceHeartbeatInfo)
		if ok && instanceInfo != nil {
			ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout)
			defer cancel()
			err := cleanInstance(ctx, instanceInfo.serviceID, instanceInfo.instanceID)
			if err != nil {
				log.Error("failed to cleanInstance in mongodb.", err)
			}
		}
	})

	for i := 1; i <= workerNum; i++ {
		gopool.Go(func(ctx context.Context) {
			for {
				select {
				case <-ctx.Done():
					log.Warn("heartbeat work protocol exit.")
					return
				case heartbeatInfo, ok := <-cacheChan:
					if ok {
						instanceHeartbeatStore.Set(heartbeatInfo.instanceID, heartbeatInfo, time.Duration(heartbeatInfo.ttl)*time.Second)
					}
				}
			}
		})
	}
}

func addHeartbeatTask(serviceID string, instanceID string, ttl int32) error {
	// Unassigned setting default value is 30s
	if ttl <= 0 {
		ttl = defaultTTL
	}
	newInstance := &instanceHeartbeatInfo{
		serviceID:   serviceID,
		instanceID:  instanceID,
		ttl:         ttl,
		lastRefresh: time.Now(),
	}
	select {
	case cacheChan <- newInstance:
		return nil
	case <-time.After(time.Duration(heartbeatTaskTimeout) * time.Second):
		log.Warn("the heartbeat's channel is full. ")
		return ErrHeartbeatTimeout
	}
}

func RemoveCacheInstance(instanceID string) {
	instanceHeartbeatStore.Delete(instanceID)
}

func cleanInstance(ctx context.Context, serviceID string, instanceID string) error {
	session, err := client.GetMongoClient().StartSession(ctx)
	if err != nil {
		return err
	}
	if err = session.StartTransaction(); err != nil {
		return err
	}
	defer session.EndSession(ctx)

	filter := bson.M{
		mongo.StringBuilder([]string{db.ColumnInstance, db.ColumnServiceID}):  serviceID,
		mongo.StringBuilder([]string{db.ColumnInstance, db.ColumnInstanceID}): instanceID,
	}

	result, err := client.GetMongoClient().FindOne(ctx, db.CollectionInstance, filter)
	if err != nil {
		log.Error("failed to query instance: %v", err)
		return err
	}
	var ins db.Instance
	err = result.Decode(&ins)
	if err != nil {
		log.Error("decode instance failed: %v", err)
		return err
	}
	ttl := ins.Instance.HealthCheck.Interval * (ins.Instance.HealthCheck.Times + 1)
	if ttl <= 0 {
		ttl = defaultTTL
	}
	if isOutDate(ins.RefreshTime, ttl) {
		return nil
	}
	err = removeDBInstance(ctx, ins.Instance.ServiceId, ins.Instance.InstanceId)
	if err != nil {
		log.Error("fail to remote instance in db: %v", err)
		errAbort := session.AbortTransaction(ctx)
		if errAbort != nil {
			return errAbort
		}
		return err
	}
	err = session.CommitTransaction(ctx)
	return err
}

func removeDBInstance(ctx context.Context, serviceID string, instanceID string) error {
	filter := bson.M{
		mongo.StringBuilder([]string{db.ColumnInstance, db.ColumnServiceID}):  serviceID,
		mongo.StringBuilder([]string{db.ColumnInstance, db.ColumnInstanceID}): instanceID,
	}
	res, err := client.GetMongoClient().DeleteOne(ctx, db.CollectionInstance, filter)
	if err != nil {
		log.Error("failed to clean instance", err)
		return err
	}
	log.Info(fmt.Sprintf("delete from mongodb:%+v", res))
	return nil
}

func findInstance(ctx context.Context, serviceID string, instanceID string) (*db.Instance, error) {
	filter := bson.M{
		mongo.StringBuilder([]string{db.ColumnInstance, db.ColumnServiceID}):  serviceID,
		mongo.StringBuilder([]string{db.ColumnInstance, db.ColumnInstanceID}): instanceID,
	}
	result, err := client.GetMongoClient().FindOne(ctx, db.CollectionInstance, filter)
	if err != nil {
		return nil, err
	}
	var ins db.Instance
	err = result.Decode(&ins)
	if err != nil {
		log.Error("decode instance failed: ", err)
		return nil, err
	}
	return &ins, nil
}

func updateInstance(ctx context.Context, serviceID string, instanceID string) error {
	filter := bson.M{
		mongo.StringBuilder([]string{db.ColumnInstance, db.ColumnServiceID}):  serviceID,
		mongo.StringBuilder([]string{db.ColumnInstance, db.ColumnInstanceID}): instanceID,
	}
	update := bson.M{
		"$set": bson.M{db.ColumnRefreshTime: time.Now()},
	}
	result, err := client.GetMongoClient().FindOneAndUpdate(ctx, db.CollectionInstance, filter, update)
	if err != nil {
		log.Error("failed to update refresh time of instance: ", err)
		return err
	}
	return result.Err()
}

func isOutDate(refreshTime time.Time, ttl int32) bool {
	return time.Since(refreshTime) <= time.Duration(ttl)*time.Second
}
