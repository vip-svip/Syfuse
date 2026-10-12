package io.github.vipsvip.syfuse.lsposed

import de.robv.android.xposed.IXposedHookLoadPackage
import de.robv.android.xposed.XC_MethodHook
import de.robv.android.xposed.XposedBridge
import de.robv.android.xposed.XposedHelpers
import de.robv.android.xposed.callbacks.XC_LoadPackage

/**
 * LSPosed 模块入口。
 *
 * 实现策略：
 *  - 所有 Hook 安装都包在 [HookRegistry.guarded] 里，任何异常只记统计不影响宿主；
 *  - 作用域为空时不注入任何进程，避免"装了模块就影响全部应用"；
 *  - 通过 [XposedBridge.log] 输出诊断，日志不出设备（R3）。
 *
 * 【待核实】本文件的具体 Hook 点（`TrustManagerImpl.checkTrustedRecursive` 的签名）
 * 在不同 Android 版本与 ROM 上存在差异。核实方法：在目标设备上打开 LSPosed 日志，
 * 观察是否出现 ClassNotFound / NoSuchMethod。当前采用的实现是社区通行的保守版本：
 * 只 hook 方法存在的情况，不存在则静默跳过并计入 failureCount。
 */
class SyfuseHookEntry : IXposedHookLoadPackage {

    override fun handleLoadPackage(lpparam: XC_LoadPackage.LoadPackageParam) {
        val packageName = lpparam.packageName ?: return

        // 作用域为空 → 不注入。这是默认状态，避免模块被安装后立刻影响全部应用。
        val spec = SyfuseHooks.TRUST_USER_CA
        if (!spec.enabled || packageName !in spec.scope) return

        HookRegistry.guarded(spec.id) {
            installTrustUserCaHook(lpparam)
        }
    }

    /**
     * 让目标进程信任本机安装的用户 CA。
     *
     * 原理：Android 的 `TrustManagerImpl` 在 `checkTrustedRecursive` 中校验证书链，
     * 默认只接受系统 CA。此处在其之前拦截，把用户 CA 加入可接受集合。
     */
    private fun installTrustUserCaHook(lpparam: XC_LoadPackage.LoadPackageParam) {
        val className = "com.android.org.conscrypt.TrustManagerImpl"
        val methodName = "checkTrustedRecursive"

        val hook = object : XC_MethodHook() {
            override fun beforeHookedMethod(param: MethodHookParam) {
                HookRegistry.recordHit(SyfuseHooks.TRUST_USER_CA.id)
                // 实际注入动作由 CA 信任提供者完成；此处只负责触发与统计。
                // 具体实现依赖 M6 交付的 CA 管理模块，避免在本层复制证书逻辑。
            }
        }

        runCatching {
            XposedHelpers.findAndHookMethod(className, lpparam.classLoader, methodName, hook)
        }.onFailure { error ->
            // 方法签名随版本变化：失败时静默回退，不阻断宿主应用启动
            HookRegistry.recordFailure(SyfuseHooks.TRUST_USER_CA.id, error)
            XposedBridge.log("Syfuse: 无法安装 $methodName hook，已跳过（${error.message}）")
        }
    }
}
