// Copyright 2020 The SQLFlow Authors. All rights reserved.
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

package pai

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"sqlflow.org/sqlflow/pkg/ir"
	pb "sqlflow.org/sqlflow/pkg/proto"
	"sqlflow.org/sqlflow/pkg/sql/codegen/tensorflow"
)

var dataSource = "maxcompute://test:test@service-maxcompute.com/api?curr_project=test&scheme=http"

var exportedLocal = []string{
	"feature_columns",
	"feature_column_names",
	"feature_metas",
	"model_params",
}

var knownTrainParams = append(
	[]string{
		"is_keras_model",
		"datasource",
		"estimator",
		"select",
		"validate_select",
		"save",
		"batch_size",
		"epochs",
		"verbose",
	},
	exportedLocal...)

var knownPredictParams = append(
	[]string{
		"result_table",
		"hdfs_namenode_addr",
		"hive_location",
		"hdfs_user",
		"hdfs_pass",
	},
	knownTrainParams...,
)

func contains(l []string, s string) bool {
	for _, v := range l {
		if v == s {
			return true
		}
	}
	return false
}

func hasExportedLocal(code string) bool {
	for _, v := range exportedLocal {
		r := regexp.MustCompile(fmt.Sprintf(`\b%[1]s=%[1]s,`, v))
		if len(r.FindStringIndex(code)) <= 0 {
			return false
		}
	}
	return true
}

func hasUnknownParameters(code string, list []string) bool {
	r := regexp.MustCompile(`(?s)((?:\bpred\(|\btrain\().*)`)
	c := r.FindStringSubmatch(code)[1]
	r = regexp.MustCompile(`[(\s,](\w+)=.*,`)
	for _, v := range r.FindStringSubmatch(c)[1:] {
		if !contains(list, v) {
			return true
		}

	}
	return false
}

func mockClusterConfig() *ClusterConfig {
	return &ClusterConfig{
		PS: PSConfig{
			Count: 0,
			CPU:   200,
			GPU:   0,
		},
		Worker: WorkerConfig{
			Count: 0,
			CPU:   200,
			GPU:   0,
		},
	}
}

func mockSession() *pb.Session {
	return &pb.Session{DbConnStr: "mysql://root:root@tcp(127.0.0.1:3306)/?maxAllowedPacket=0"}
}

func TestGetPAICmd(t *testing.T) {
	a := assert.New(t)
	cc := &ClusterConfig{
		Worker: WorkerConfig{
			Count: 1,
			CPU:   2,
			GPU:   0,
		},
		PS: PSConfig{
			Count: 2,
			CPU:   4,
			GPU:   0,
		},
	}
	os.Setenv("SQLFLOW_OSS_CHECKPOINT_DIR", "oss://bucket/?role_arn=xxx&host=xxx")
	defer os.Unsetenv("SQLFLOW_OSS_CHECKPOINT_DIR")
	paiCmd, err := getTFPAICmd(cc, "file:///tmp/task.tar.gz", "my_model", "project/12345/my_model", "testdb.test", "", "testdb.result")
	a.NoError(err)
	ckpDir, err := FormatCkptDir("project/12345/my_model")
	a.NoError(err)
	expected := fmt.Sprintf("pai -name tensorflow1120 -DjobName=sqlflow_my_model -Dtags=dnn -Dscript=file:///tmp/task.tar.gz -DentryFile=entry.py -Dtables=odps://testdb/tables/test -Doutputs=odps://testdb/tables/result -DcheckpointDir=\"%s\"", ckpDir)
	a.Equal(expected, paiCmd)
}

func TestTrainCodegen(t *testing.T) {
	a := assert.New(t)
	trainStmt := ir.MockTrainStmt(false)

	os.Setenv("SQLFLOW_OSS_CHECKPOINT_DIR", "oss://bucket/?role_arn=xxx&host=xxx")
	defer os.Unsetenv("SQLFLOW_OSS_CHECKPOINT_DIR")

	sess := mockSession()
	paiTfCode, err := TFTrainAndSave(trainStmt, sess, "my_dnn_model", mockClusterConfig())
	a.NoError(err)

	tfCode, err := tensorflow.Train(trainStmt, sess)
	a.NoError(err)

	a.True(strings.HasPrefix(paiTfCode, tfCode))
	a.True(hasExportedLocal(tfCode))
	a.False(hasUnknownParameters(paiTfCode, knownTrainParams))
}

func TestPredictCodegen(t *testing.T) {
	a := assert.New(t)
	ir := ir.MockPredStmt(ir.MockTrainStmt(false))

	os.Setenv("SQLFLOW_OSS_CHECKPOINT_DIR", "oss://bucket/?role_arn=xxx&host=xxx")
	defer os.Unsetenv("SQLFLOW_OSS_CHECKPOINT_DIR")
	sess := mockSession()
	paiTfCode, err := TFLoadAndPredict(ir, sess, "my_dnn_model")
	a.NoError(err)
	a.False(hasUnknownParameters(paiTfCode, knownPredictParams))

	session := &pb.Session{
		Token:            "",
		DbConnStr:        "",
		ExitOnSubmit:     false,
		UserId:           "",
		HiveLocation:     "/sqlflowtmp",
		HdfsNamenodeAddr: "192.168.1.1:8020",
		HdfsUser:         "sqlflow_admin",
		HdfsPass:         "sqlflow_pass",
	}

	tfCode, err := tensorflow.Pred(ir, session)
	a.NoError(err)

	a.True(hasExportedLocal(tfCode))
	a.False(hasUnknownParameters(tfCode, knownPredictParams))
}
