<!--
  提交 PR 前请先读 CONTRIBUTING.md。下面每一项都不是形式主义：
  这个项目的核心纪律是"不编造"，而这些检查点就是它的执行方式。
-->

## 这个 PR 做了什么

<!-- 一到三句。说清"改了什么"和"为什么"。 -->

## 为什么需要它

<!-- 解决什么问题？关联 issue：Fixes #... -->

## 验证证据

> **贴原始命令输出，不要只写结论。** 这是本仓库最重要的一条要求。

```
$ tests/run_unit.sh
（粘贴真实输出）
```

```
$ tests/run_module_checks.sh
（粘贴真实输出）
```

```
$ tests/run_kernel.sh          # 若改了 kernel/
（粘贴真实输出）
```

```
$ tests/ui_assets_check.sh     # 若改了界面或资源
（粘贴真实输出）
```

<!-- 若改了界面，请附截图（tests/ui_screenshot.sh 的产出） -->

## 提交前自检

- [ ] `tests/run_unit.sh` 通过
- [ ] `tests/run_module_checks.sh` 通过（FAIL=0）
- [ ] `tests/run_kernel.sh` 通过（若改了 `kernel/`）
- [ ] `tests/ui_assets_check.sh` 通过（若改了界面/资源）
- [ ] `python tools/scan_secrets.py` 通过
- [ ] **我没有把"跳过（exit 3）"写成"通过"**

## 硬约束

- [ ] R1 未引入 WebView
- [ ] R2 未引入运行时下载可执行内容
- [ ] R3 未引入遥测 / 崩溃上报 / 广告 SDK
- [ ] R4 未声称未验证的结论，未引入 `NotImplementedError` / `TODO(implement)` 占位
- [ ] R5 未提交任何真实凭据、`*.jks`、`*.keystore`、`local.properties`
- [ ] R6 若新增危险能力，已同步更新 `DISCLAIMER.md` 并提供还原路径

## 影响面

- [ ] 规则语义有变化 → 已在 `tests/expect.tsv` 与 `tests/corpus/` 补对应用例
- [ ] 新增依赖 → 已更新 `NOTICE`，且许可证与 GPL-3.0-or-later 兼容
- [ ] 配置格式有变化 → 已递增 `schemaVersion` 并说明迁移方式
- [ ] Room schema 有变化 → 已提供迁移，`data/schemas/` 保留历史 schema
- [ ] 版本号有变化 → `app/build.gradle.kts` 与 `module/module.prop` 已同步
- [ ] 以上均不适用

## 已知未做到的部分

<!--
  如果这个 PR 有未完成的部分、绕过的坑、或你不确定的地方，写在这里。
  留空也可以，但不要为了让 PR 看起来完美而隐瞒。
  项目更怕"看起来做完了实际没做完"，而不是"明确说还没做完"。
-->

## 类型

- [ ] `feat` 新功能
- [ ] `fix` 缺陷修复
- [ ] `perf` 性能
- [ ] `refactor` 重构（无行为变化）
- [ ] `docs` 文档
- [ ] `test` 测试
- [ ] `chore` 构建 / CI / 杂项
