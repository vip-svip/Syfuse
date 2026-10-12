## 变更说明

<!-- 这个 PR 做了什么，为什么 -->

## 关联 Issue

<!-- 如 Closes #123 -->

## 变更类型

- [ ] `feat` 新功能
- [ ] `fix` 缺陷修复
- [ ] `perf` 性能优化
- [ ] `refactor` 重构
- [ ] `docs` 文档
- [ ] `test` 测试
- [ ] `build` / `ci` 构建与 CI
- [ ] `chore` 杂项

## 红线自查

<!-- 以下六条是不可协商的约束，请逐项确认 -->

- [ ] **R1** 未引入 WebView / H5 / 混合框架承载主界面
- [ ] **R2** 内核仍为静态内置，未新增运行时下载内核二进制的逻辑
- [ ] **R3** 未引入遥测、崩溃上报或任何未告知的上报
- [ ] **R4** 未编造 API、常量或性能数字；未把推断写成实测
- [ ] **R5** 未提交任何密钥、`*.jks`、`local.properties`
- [ ] **R6** 新增的危险能力有风险说明 + 二次确认 + 还原路径

## 验证

<!-- 你跑了哪些验证？把命令与输出贴上 -->

```
bash tests/run_module_checks.sh
python tools/scan_secrets.py
python tests/ui_assets_check.py
```

## 文档更新

- [ ] 若新增依赖，已更新 `NOTICE`
- [ ] 若变更用户可见行为，已更新 `README`
- [ ] 若变更 UI 资产，已运行 `python tests/ui_assets_check.py --update` 并说明原因

## 其他说明

<!-- 需要 reviewer 特别注意的地方、已知限制等 -->
