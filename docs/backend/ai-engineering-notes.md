# AI 工程问题与取舍

这里概括 VidLens 中几项经过迭代的 Agent 实现。详细的过程复盘、历史验证和面试笔记保存在本机私有文档，不纳入公开仓库。以下描述以提交 `4fc1d58` 的实现为基线；旧任务继续遵守创建时冻结的策略。

## 文章审核与交付

早期实现将独立来源核验作为发布前置条件；后续围绕审核分批、证据范围与修订收敛做了多轮改进。当前新文章先经过结构、来源和资源检查，再交付阅读，独立审核作为后续建议；审核失败不会被说成审核通过。选择交付优先意味着必须接受：结构检查不能证明文章语义正确，审核也不能保证发现所有问题。

相关演进：[`8878eb2`](https://github.com/junnhwan/vid-lens/commit/8878eb23b9817e9f23e8370cacc8cf6abe982eeb) · [`3eebdae`](https://github.com/junnhwan/vid-lens/commit/3eebdaea1186324145e614beb7ef85af802f4051) · [`ae1ce40`](https://github.com/junnhwan/vid-lens/commit/ae1ce40bb39f4d431a05daf7852dca806475eaaa)。

## 长文输出损坏与恢复

模型工具输出可能不是完整 JSON。文章循环按章节保存有版本的草稿；输出损坏时，保留已保存内容，以较小的后续工作范围继续，而不是将损坏文本当成写作模板。剩余来源和整体校验仍有义务完成，调用、时间与无进展预算也不会因恢复而重置。

实现见[文章循环](../../internal/investigator/article_loop.go)、[交付恢复](../../internal/investigator/article_delivery.go)和[恢复测试](../../internal/investigator/article_delivery_test.go)。

## 按需截图与正文预算

作者在保存正文时提出具体的画面需求，程序有界取帧并将实际像素交付给后续写作；正文所需调用和时间优先预留。合理的宽时间范围可以在本地收窄，失败或预算不足时省略可选图；所用画面仍须保持真实来源与时间。图片送达只证明有视觉输入，不证明图注理解正确。

相关演进：[`a60f6fb`](https://github.com/junnhwan/vid-lens/commit/a60f6fbdc1dcdd292a5be694c39ac273bfb0c7c9) · [视觉实现](../../internal/investigator/article_delivery_visual.go)。

后续只补充有代表性的实现与权衡；涉及历史行为或验证结果时，应注明版本、证据和未验证边界。
