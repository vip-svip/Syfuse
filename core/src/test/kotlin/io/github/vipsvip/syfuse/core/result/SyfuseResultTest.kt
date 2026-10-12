package io.github.vipsvip.syfuse.core.result

import com.google.common.truth.Truth.assertThat
import org.junit.jupiter.api.Test

/**
 * [SyfuseResult] 与 [SyfuseError] 的单测。
 *
 * 重点覆盖失败传播：原因是全项目都用这个类型传递可预期错误，
 * 一旦某个组合函数把 Failure 吞成 Success，会导致「静默失败」
 * （提示词 2.3 第 6 条明令禁止）。
 */
class SyfuseResultTest {

    @Test
    fun `Success 的 isSuccess 为真且 getOrNull 返回原值`() {
        val result: SyfuseResult<Int> = SyfuseResult.Success(42)
        assertThat(result.isSuccess).isTrue()
        assertThat(result.getOrNull()).isEqualTo(42)
    }

    @Test
    fun `Failure 的 isSuccess 为假且 getOrNull 返回 null`() {
        val result: SyfuseResult<Int> = SyfuseResult.Failure(
            SyfuseError.Validation("port", "端口超出范围"),
        )
        assertThat(result.isSuccess).isFalse()
        assertThat(result.getOrNull()).isNull()
    }

    @Test
    fun `map 变换成功值`() {
        val result = SyfuseResult.Success(21).map { it * 2 }
        assertThat(result.getOrNull()).isEqualTo(42)
    }

    @Test
    fun `map 不吞掉失败`() {
        val failure = SyfuseResult.Failure(SyfuseError.Engine("内核启动失败", exitCode = 1))
        val mapped = failure.map { value: Int -> value * 2 }

        assertThat(mapped).isInstanceOf(SyfuseResult.Failure::class.java)
        assertThat((mapped as SyfuseResult.Failure).error).isEqualTo(failure.error)
    }

    @Test
    fun `Parse 错误保留行列信息用于界面定位`() {
        val error = SyfuseError.Parse(
            source = "rules.conf",
            message = "未知的规则类型",
            line = 17,
            column = 5,
        )
        // 提示词 5.3 要求非法文件给出精确错误位置，故行列必须可取
        assertThat(error.line).isEqualTo(17)
        assertThat(error.column).isEqualTo(5)
    }

    @Test
    fun `NotImplemented 的 message 自动包含功能名`() {
        val error = SyfuseError.NotImplemented("延迟测速")
        assertThat(error.message).contains("延迟测速")
    }

    @Test
    fun `Permission 错误携带能力名便于引导授权`() {
        val error = SyfuseError.Permission("VPN", "用户拒绝了 VPN 授权")
        assertThat(error.capability).isEqualTo("VPN")
    }
}
