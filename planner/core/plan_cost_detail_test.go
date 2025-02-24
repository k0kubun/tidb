// Copyright 2022 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package core_test

import (
	"context"
	"testing"

	"github.com/pingcap/tidb/domain"
	"github.com/pingcap/tidb/parser"
	"github.com/pingcap/tidb/planner/core"
	"github.com/pingcap/tidb/sessionctx"
	"github.com/pingcap/tidb/testkit"
	"github.com/pingcap/tidb/util/hint"
	"github.com/pingcap/tidb/util/plancodec"
	"github.com/pingcap/tidb/util/tracing"
	"github.com/stretchr/testify/require"
)

func TestPlanCostDetail(t *testing.T) {
	p := parser.New()
	store, dom := testkit.CreateMockStoreAndDomain(t)
	tk := testkit.NewTestKit(t, store)
	tk.MustExec("use test")
	tk.MustExec(`create table t (a int primary key, b int, c int, d int, k int, key b(b), key cd(c, d), unique key(k))`)
	testcases := []struct {
		sql        string
		assertLbls []string
		tp         string
	}{
		{
			tp:  plancodec.TypePointGet,
			sql: "select * from t where a = 1",
			assertLbls: []string{
				core.RowSizeLbl,
				core.NetworkFactorLbl,
				core.SeekFactorLbl,
			},
		},
		{
			tp:  plancodec.TypeBatchPointGet,
			sql: "select * from t where a = 1 or a = 2 or a = 3",
			assertLbls: []string{
				core.RowCountLbl,
				core.RowSizeLbl,
				core.NetworkFactorLbl,
				core.SeekFactorLbl,
				core.ScanConcurrencyLbl,
			},
		},
		{
			tp:  plancodec.TypeTableFullScan,
			sql: "select * from t",
			assertLbls: []string{
				core.RowCountLbl,
				core.RowSizeLbl,
				core.ScanFactorLbl,
			},
		},
		{
			tp:  plancodec.TypeTableReader,
			sql: "select * from t",
			assertLbls: []string{
				core.RowCountLbl,
				core.RowSizeLbl,
				core.NetworkFactorLbl,
				core.NetSeekCostLbl,
				core.TablePlanCostLbl,
				core.ScanConcurrencyLbl,
			},
		},
		{
			tp:  plancodec.TypeIndexFullScan,
			sql: "select b from t",
			assertLbls: []string{
				core.RowCountLbl,
				core.RowSizeLbl,
				core.ScanFactorLbl,
			},
		},
		{
			tp:  plancodec.TypeIndexReader,
			sql: "select b from t",
			assertLbls: []string{
				core.RowCountLbl,
				core.RowSizeLbl,
				core.NetworkFactorLbl,
				core.NetSeekCostLbl,
				core.IndexPlanCostLbl,
				core.ScanConcurrencyLbl,
			},
		},
	}
	for _, tc := range testcases {
		costDetails := optimize(t, tc.sql, p, tk.Session(), dom)
		asserted := false
		for _, cd := range costDetails {
			if cd.GetPlanType() == tc.tp {
				asserted = true
				for _, lbl := range tc.assertLbls {
					require.True(t, cd.Exists(lbl))
				}
			}
		}
		require.True(t, asserted)
	}
}

func optimize(t *testing.T, sql string, p *parser.Parser, ctx sessionctx.Context, dom *domain.Domain) map[int]*tracing.PhysicalPlanCostDetail {
	stmt, err := p.ParseOneStmt(sql, "", "")
	require.NoError(t, err)
	err = core.Preprocess(ctx, stmt, core.WithPreprocessorReturn(&core.PreprocessorReturn{InfoSchema: dom.InfoSchema()}))
	require.NoError(t, err)
	sctx := core.MockContext()
	sctx.GetSessionVars().StmtCtx.EnableOptimizeTrace = true
	sctx.GetSessionVars().EnableNewCostInterface = true
	builder, _ := core.NewPlanBuilder().Init(sctx, dom.InfoSchema(), &hint.BlockHintProcessor{})
	domain.GetDomain(sctx).MockInfoCacheAndLoadInfoSchema(dom.InfoSchema())
	plan, err := builder.Build(context.TODO(), stmt)
	require.NoError(t, err)
	_, _, err = core.DoOptimize(context.TODO(), sctx, builder.GetOptFlag(), plan.(core.LogicalPlan))
	require.NoError(t, err)
	return sctx.GetSessionVars().StmtCtx.OptimizeTracer.Physical.PhysicalPlanCostDetails
}
