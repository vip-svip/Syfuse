package io.github.vipsvip.syfuse.privileged

/**
 * 特权命令白名单。
 *
 * 6.5 的硬性安全要求是"禁止拼接用户输入执行 shell"。做法不是"小心地转义字符串"，
 * 而是**根本不构造字符串**：每个命令是一个封闭的枚举成员，参数只能取经过正则校验的值。
 * 任何不满足校验的输入在构造阶段就被拒绝，永远不会到达 shell。
 *
 * 这一点是刻意的过度防御：转义函数总会在某个 Unicode 边界上出错，
 * 而白名单不会。
 */
sealed interface PrivilegeCommand {

    /** 传给 shell 的参数数组（不含 shell 本身）。 */
    val argv: List<String>

    /** 读取一个绝对路径的文件内容。 */
    data class ReadFile(val path: String) : PrivilegeCommand {
        init {
            require(isSafePath(path)) { "非法路径：$path" }
        }

        override val argv: List<String> get() = listOf("cat", path)
    }

    /** 判断路径是否存在（-e）。 */
    data class Exists(val path: String) : PrivilegeCommand {
        init {
            require(isSafePath(path)) { "非法路径：$path" }
        }

        override val argv: List<String> get() = listOf("test", "-e", path)
    }

    /** 判断路径是否可写（-w）。 */
    data class Writable(val path: String) : PrivilegeCommand {
        init {
            require(isSafePath(path)) { "非法路径：$path" }
        }

        override val argv: List<String> get() = listOf("test", "-w", path)
    }

    /** 在 PATH 中查找可执行文件。 */
    data class Which(val binary: String) : PrivilegeCommand {
        init {
            require(isSafeBinaryName(binary)) { "非法可执行文件名：$binary" }
        }

        override val argv: List<String> get() = listOf("which", binary)
    }

    /** 读取 /proc/filesystems 以判断 overlayfs 支持。 */
    data object Filesystems : PrivilegeCommand {
        override val argv: List<String> get() = listOf("cat", "/proc/filesystems")
    }

    /** 读取 netfilter 目标列表以判断 TPROXY 支持。 */
    data object IptablesTargets : PrivilegeCommand {
        override val argv: List<String> get() = listOf("cat", "/proc/net/ip_tables_targets")
    }

    /** 读取已加载内核模块，用于判断 xt_TPROXY 是否可用。 */
    data object LoadedModules : PrivilegeCommand {
        override val argv: List<String> get() = listOf("cat", "/proc/modules")
    }

    companion object {
        /**
         * 只允许绝对路径、只允许 `[A-Za-z0-9._/-]`，且禁止 `..`。
         * 这条正则同时挡掉了空格（参数注入）、引号、`;`、`|`、`$`、反引号、换行。
         */
        private val SAFE_PATH = Regex("^/[A-Za-z0-9._/-]{0,512}$")

        private val SAFE_BINARY = Regex("^[a-z0-9][a-z0-9_-]{0,63}$")

        fun isSafePath(path: String): Boolean =
            SAFE_PATH.matches(path) && !path.split('/').contains("..")

        fun isSafeBinaryName(name: String): Boolean = SAFE_BINARY.matches(name)
    }
}
