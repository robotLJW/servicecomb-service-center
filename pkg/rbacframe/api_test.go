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

package rbacframe_test

import (
	"testing"

	"github.com/go-chassis/go-chassis/v2/security/secret"
	"github.com/go-chassis/go-chassis/v2/security/token"
	"github.com/stretchr/testify/assert"

	"github.com/apache/servicecomb-service-center/pkg/rbacframe"
)

func TestMustAuth(t *testing.T) {
	rbacframe.Add2WhiteAPIList("/test")
	assert.False(t, rbacframe.MustAuth("/test"))
	assert.True(t, rbacframe.MustAuth("/test1"))
	assert.True(t, rbacframe.MustAuth("/auth"))
	assert.False(t, rbacframe.MustAuth("/version"))
	assert.False(t, rbacframe.MustAuth("/v4/a/registry/version"))
	assert.False(t, rbacframe.MustAuth("/health"))
	assert.False(t, rbacframe.MustAuth("/v4/a/registry/health"))
}

func TestAuthenticate(t *testing.T) {
	pri, pub, err := secret.GenRSAKeyPair(4096)
	assert.NoError(t, err)

	to, err := token.Sign(map[string]interface{}{
		rbacframe.ClaimsUser:  "root",
		rbacframe.ClaimsRoles: []string{"admin"},
	}, pri, token.WithSigningMethod(token.RS512))
	assert.NoError(t, err)

	_, err = rbacframe.Authenticate(to, pub)
	assert.NoError(t, err)

	_, err = rbacframe.Authenticate("token", nil)
	assert.Error(t, err)
}
