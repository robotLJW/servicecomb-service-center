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
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/go-chassis/cari/discovery"
	"go.mongodb.org/mongo-driver/bson"

	"github.com/apache/servicecomb-service-center/datasource/mongo/client"
	"github.com/apache/servicecomb-service-center/datasource/mongo/db"
	"github.com/apache/servicecomb-service-center/pkg/log"
	"github.com/apache/servicecomb-service-center/pkg/util"
)

func GetRulesUtil(ctx context.Context, domain string, project string, serviceID string) ([]*db.Rule, error) {
	filter := bson.M{
		db.ColumnDomain:    domain,
		db.ColumnProject:   project,
		db.ColumnServiceID: serviceID,
	}
	cursor, err := client.GetMongoClient().Find(ctx, db.CollectionRule, filter)
	if err != nil {
		return nil, err
	}
	if cursor.Err() != nil {
		return nil, cursor.Err()
	}
	var rules []*db.Rule
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var rule db.Rule
		err := cursor.Decode(&rule)
		if err != nil {
			log.Error("type conversion error", err)
			return nil, err
		}
		rules = append(rules, &rule)
	}
	return rules, nil
}

func Filter(ctx context.Context, rules []*db.Rule, consumerID string) (bool, error) {
	consumer, err := GetServiceByID(ctx, consumerID)
	if consumer == nil {
		return false, err
	}

	if len(rules) == 0 {
		return true, nil
	}
	domain := util.ParseDomainProject(ctx)
	project := util.ParseProject(ctx)

	tags, err := GetTags(ctx, domain, project, consumerID)
	if err != nil {
		return false, err
	}
	matchErr := MatchRules(rules, consumer.Service, tags)
	if matchErr != nil {
		if matchErr.Code == discovery.ErrPermissionDeny {
			return false, nil
		}
		return false, matchErr
	}
	return true, nil
}

func FilterAll(ctx context.Context, consumerIDs []string, rules []*db.Rule) (allow []string, deny []string, err error) {
	l := len(consumerIDs)
	if l == 0 || len(rules) == 0 {
		return consumerIDs, nil, nil
	}

	allowIdx, denyIdx := 0, l
	consumers := make([]string, l)
	for _, consumerID := range consumerIDs {
		ok, err := Filter(ctx, rules, consumerID)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			consumers[allowIdx] = consumerID
			allowIdx++
		} else {
			denyIdx--
			consumers[denyIdx] = consumerID
		}
	}
	return consumers[:allowIdx], consumers[denyIdx:], nil
}

func MatchRules(rulesOfProvider []*db.Rule, consumer *discovery.MicroService, tagsOfConsumer map[string]string) *discovery.Error {
	if consumer == nil {
		return discovery.NewError(discovery.ErrInvalidParams, "consumer is nil")
	}

	if len(rulesOfProvider) <= 0 {
		return nil
	}
	if rulesOfProvider[0].Rule.RuleType == "WHITE" {
		return patternWhiteList(rulesOfProvider, tagsOfConsumer, consumer)
	}
	return patternBlackList(rulesOfProvider, tagsOfConsumer, consumer)
}

func patternWhiteList(rulesOfProvider []*db.Rule, tagsOfConsumer map[string]string, consumer *discovery.MicroService) *discovery.Error {
	v := reflect.Indirect(reflect.ValueOf(consumer))
	consumerID := consumer.ServiceId
	for _, rule := range rulesOfProvider {
		value, err := parsePattern(v, rule.Rule, tagsOfConsumer, consumerID)
		if err != nil {
			return err
		}
		if len(value) == 0 {
			continue
		}

		match, _ := regexp.MatchString(rule.Rule.Pattern, value)
		if match {
			log.Info(fmt.Sprintf("consumer[%s][%s/%s/%s/%s] match white list, rule.Pattern is %s, value is %s",
				consumerID, consumer.Environment, consumer.AppId, consumer.ServiceName, consumer.Version,
				rule.Rule.Pattern, value))
			return nil
		}
	}
	return discovery.NewError(discovery.ErrPermissionDeny, "not found in white list")
}

func patternBlackList(rulesOfProvider []*db.Rule, tagsOfConsumer map[string]string, consumer *discovery.MicroService) *discovery.Error {
	v := reflect.Indirect(reflect.ValueOf(consumer))
	consumerID := consumer.ServiceId
	for _, rule := range rulesOfProvider {
		var value string
		value, err := parsePattern(v, rule.Rule, tagsOfConsumer, consumerID)
		if err != nil {
			return err
		}
		if len(value) == 0 {
			continue
		}

		match, _ := regexp.MatchString(rule.Rule.Pattern, value)
		if match {
			log.Warn(fmt.Sprintf("no permission to access, consumer[%s][%s/%s/%s/%s] match black list, rule.Pattern is %s, value is %s",
				consumerID, consumer.Environment, consumer.AppId, consumer.ServiceName, consumer.Version,
				rule.Rule.Pattern, value))
			return discovery.NewError(discovery.ErrPermissionDeny, "found in black list")
		}
	}
	return nil
}

func parsePattern(v reflect.Value, rule *discovery.ServiceRule, tagsOfConsumer map[string]string, consumerID string) (string, *discovery.Error) {
	if strings.HasPrefix(rule.Attribute, "tag_") {
		key := rule.Attribute[4:]
		value := tagsOfConsumer[key]
		if len(value) == 0 {
			log.Info(fmt.Sprintf("can not find service[%s] tag[%s]", consumerID, key))
		}
		return value, nil
	}
	key := v.FieldByName(rule.Attribute)
	if !key.IsValid() {
		log.Error(fmt.Sprintf("can not find service[%s] field[%s], ruleID is %s",
			consumerID, rule.Attribute, rule.RuleId), nil)
		return "", discovery.NewError(discovery.ErrInternal, fmt.Sprintf("can not find field '%s'", rule.Attribute))
	}
	return key.String(), nil

}
