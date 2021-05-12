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

package gov_test

import (
	_ "github.com/apache/servicecomb-service-center/server/service/gov/mock"

	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/apache/servicecomb-service-center/pkg/gov"
	"github.com/apache/servicecomb-service-center/server/config"
	svc "github.com/apache/servicecomb-service-center/server/service/gov"
)

const Project = "default"
const MockKind = "default"
const MatchGroup = "match-group"
const MockEnv = ""
const MockApp = ""

var id = ""

func init() {
	config.App = &config.AppConfig{
		Gov: &config.Gov{
			DistOptions: []config.DistributorOptions{
				{
					Name: "mockServer",
					Type: "mock",
				},
			},
		},
	}
	err := svc.Init()
	if err != nil {
		panic(err)
	}
}

func TestCreate(t *testing.T) {
	b, _ := json.MarshalIndent(&gov.Policy{
		GovernancePolicy: &gov.GovernancePolicy{
			Name: "Traffic2adminAPI",
			Selector: &gov.Selector{
				App:         MockApp,
				Environment: MockEnv,
			},
		},
		Spec: &gov.LBSpec{RetryNext: 3, MarkerName: "traffic2adminAPI"},
	}, "", "  ")
	res, err := svc.Create(MockKind, Project, b)
	id = string(res)
	assert.NoError(t, err)
	assert.NotEmpty(t, id)
}

func TestUpdate(t *testing.T) {
	b, _ := json.MarshalIndent(&gov.Policy{
		GovernancePolicy: &gov.GovernancePolicy{
			Name: "Traffic2adminAPI",
			Selector: &gov.Selector{
				App:         MockApp,
				Environment: MockEnv,
			},
		},
		Spec: &gov.LBSpec{RetryNext: 3, MarkerName: "traffic2adminAPI"},
	}, "", "  ")
	err := svc.Update(MockKind, id, Project, b)
	assert.NoError(t, err)
}

func TestDisplay(t *testing.T) {
	b, _ := json.MarshalIndent(&gov.Policy{
		GovernancePolicy: &gov.GovernancePolicy{
			Name: "Traffic2adminAPI",
			Selector: &gov.Selector{
				App:         MockApp,
				Environment: MockEnv,
			},
		},
	}, "", "  ")
	res, err := svc.Create(MatchGroup, Project, b)
	id = string(res)
	assert.NoError(t, err)
	policies := &[]*gov.DisplayData{}
	res, err = svc.Display(Project, MockApp, MockEnv)
	assert.NoError(t, err)
	err = json.Unmarshal(res, policies)
	assert.NoError(t, err)
	assert.NotEmpty(t, policies)
}

func TestList(t *testing.T) {
	policies := &[]*gov.Policy{}
	res, err := svc.List(MockKind, Project, MockApp, MockEnv)
	assert.NoError(t, err)
	err = json.Unmarshal(res, policies)
	assert.NoError(t, err)
	assert.NotEmpty(t, policies)
}

func TestGet(t *testing.T) {
	policy := &gov.Policy{}
	res, err := svc.Get(MockKind, id, Project)
	assert.NoError(t, err)
	err = json.Unmarshal(res, policy)
	assert.NoError(t, err)
	assert.NotNil(t, policy)
}

func TestDelete(t *testing.T) {
	err := svc.Delete(MockKind, id, Project)
	assert.NoError(t, err)
	res, _ := svc.Get(MockKind, id, Project)
	assert.Nil(t, res)
}
