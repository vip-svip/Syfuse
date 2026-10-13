package io.github.vipsvip.syfuse.core.rule

import com.google.common.truth.Truth.assertThat
import org.junit.jupiter.api.Test

class DomainTrieTest {

    @Test
    fun `后缀匹配命中自身与子域且最长匹配优先`() {
        val trie = DomainTrie<String>()
        trie.put("example.com", "wide")
        trie.put("ads.example.com", "narrow")

        val hit = trie.lookup("x.ads.example.com")
        assertThat(hit).containsExactly("narrow", "wide").inOrder()
    }

    @Test
    fun `标签级匹配不产生越界命中`() {
        val trie = DomainTrie<String>()
        trie.put("example.com", "rule")

        assertThat(trie.lookup("notexample.com")).isEmpty()
        assertThat(trie.lookup("example.com.evil.test")).isEmpty()
        assertThat(trie.lookup("example.com")).containsExactly("rule")
    }

    @Test
    fun `hostMatchesDomain 为标签级比较`() {
        assertThat(DomainTrie.hostMatchesDomain("a.example.com", "example.com")).isTrue()
        assertThat(DomainTrie.hostMatchesDomain("example.com", "example.com")).isTrue()
        assertThat(DomainTrie.hostMatchesDomain("notexample.com", "example.com")).isFalse()
        assertThat(DomainTrie.hostMatchesDomain("example.com.evil.io", "example.com")).isFalse()
        // 大小写与结尾点应被规范化
        assertThat(DomainTrie.hostMatchesDomain("A.Example.COM.", "example.com")).isTrue()
    }

    @Test
    fun `同一域名可挂多条规则且全部返回`() {
        val trie = DomainTrie<Int>()
        trie.put("dup.test", 1)
        trie.put("dup.test", 2)

        assertThat(trie.lookup("dup.test")).containsExactly(1, 2)
    }
}

class Ipv4CidrTrieTest {

    @Test
    fun `最长前缀匹配`() {
        val trie = Ipv4CidrTrie<String>()
        trie.put("10.0.0.0/8", "wide")
        trie.put("10.10.0.0/16", "narrow")

        assertThat(trie.lookup("10.10.5.5")).isEqualTo("narrow")
        assertThat(trie.lookup("10.20.5.5")).isEqualTo("wide")
        assertThat(trie.lookup("192.168.1.1")).isNull()
    }

    @Test
    fun `拒绝非法地址与非法前缀`() {
        val trie = Ipv4CidrTrie<String>()
        assertThat(trie.put("999.1.1.1", "x")).isFalse()
        assertThat(trie.put("1.2.3", "x")).isFalse()
        assertThat(trie.put("1.2.3.4/33", "x")).isFalse()
        assertThat(trie.put("1.2.3.4/24", "ok")).isTrue()
        assertThat(trie.lookup("1.2.3.4")).isEqualTo("ok")
        assertThat(trie.lookup("not-an-ip")).isNull()
    }

    @Test
    fun `斜杠 0 前缀匹配全部`() {
        val trie = Ipv4CidrTrie<String>()
        trie.put("0.0.0.0/0", "all")
        assertThat(trie.lookup("203.0.113.9")).isEqualTo("all")
    }
}

class CosmeticRuleTest {

    private fun parse(line: String, lineNo: Int = 1) = CosmeticRule.parse(line, lineNo, "test")

    @Test
    fun `六种元素类标记被正确区分`() {
        assertThat(parse("example.test##.ad")!!.type).isEqualTo(CosmeticType.HIDE)
        assertThat(parse("example.test#@#.ad")!!.type).isEqualTo(CosmeticType.UNHIDE)
        assertThat(parse("example.test#\$#.ad { display:none }")!!.type).isEqualTo(CosmeticType.CSS_INJECT)
        assertThat(parse("example.test#?#div:has(> .sponsor)")!!.type).isEqualTo(CosmeticType.EXTENDED_CSS)
        assertThat(parse("example.test#%#//scriptlet('x')")!!.type).isEqualTo(CosmeticType.SCRIPTLET)
        assertThat(parse("example.test##^script:has-text(ads)")!!.type).isEqualTo(CosmeticType.HTML_FILTER)
    }

    @Test
    fun `HTML 过滤标记不会被 HIDE 抢先切分`() {
        val rule = parse("example.test##^script:has-text(ads)")!!
        assertThat(rule.body).isEqualTo("script:has-text(ads)")
        assertThat(rule.scope.include).containsExactly("example.test")
    }

    @Test
    fun `作用域支持多域名与排除`() {
        val rule = parse("a.test,b.test,~c.a.test##.ad")!!
        assertThat(rule.scope.include).containsExactly("a.test", "b.test")
        assertThat(rule.scope.exclude).containsExactly("c.a.test")
        assertThat(rule.appliesTo("a.test")).isTrue()
        assertThat(rule.appliesTo("c.a.test")).isFalse()
        assertThat(rule.appliesTo("other.test")).isFalse()
    }

    @Test
    fun `全局规则无域名限定`() {
        val rule = parse("##.ad-global")!!
        assertThat(rule.isGlobal).isTrue()
        assertThat(rule.appliesTo("anything.test")).isTrue()
    }

    @Test
    fun `非元素类行返回 null`() {
        assertThat(parse("||example.com^")).isNull()
        assertThat(parse("0.0.0.0 ads.test")).isNull()
    }
}

class RuleParserTest {

    @Test
    fun `正则可选修饰符的切分不破坏正则锚点`() {
        val (pattern, options) = RuleParser.splitPatternAndModifiers("/ads\\d+\$/\$script")
        assertThat(pattern).isEqualTo("/ads\\d+\$/")
        assertThat(options).containsExactly("script")
    }

    @Test
    fun `普通模式的修饰符切分`() {
        val (pattern, options) = RuleParser.splitPatternAndModifiers("||a.test^\$important,script")
        assertThat(pattern).isEqualTo("||a.test^")
        assertThat(options).containsExactly("important", "script")
    }

    @Test
    fun `无修饰符时返回空列表`() {
        val (pattern, options) = RuleParser.splitPatternAndModifiers("||a.test^")
        assertThat(pattern).isEqualTo("||a.test^")
        assertThat(options).isEmpty()
    }

    @Test
    fun `domain 修饰符区分包含与排除`() {
        val outcome = RuleParser.parseLine("||a.test^\$domain=b.test|~c.test", 7, "src")
        val rule = (outcome as ParseOutcome.Network).rule
        val scope = rule.mods.domainScope!!
        assertThat(scope.include).containsExactly("b.test")
        assertThat(scope.exclude).containsExactly("c.test")
    }

    @Test
    fun `资源类型可叠加且 all 展开为全集`() {
        val two = (RuleParser.parseLine("||a.test^\$script,image", 1) as ParseOutcome.Network).rule
        assertThat(two.mods.resourceTypes)
            .containsExactly(ResourceType.SCRIPT, ResourceType.IMAGE)

        val all = (RuleParser.parseLine("||a.test^\$all", 1) as ParseOutcome.Network).rule
        assertThat(all.mods.resourceTypes).isEqualTo(ResourceType.ALL)
    }

    @Test
    fun `xhr 是 xmlhttprequest 的别名`() {
        val rule = (RuleParser.parseLine("||a.test^\$xhr", 1) as ParseOutcome.Network).rule
        assertThat(rule.mods.resourceTypes).containsExactly(ResourceType.XMLHTTPREQUEST)
    }

    @Test
    fun `匹配大小写修饰符进入正则编译`() {
        val rule = (RuleParser.parseLine("||a.test/Ads/\$match-case", 1) as ParseOutcome.Network).rule
        val regex = (rule.pattern as RulePattern.RegexPattern).regex
        assertThat(regex.containsMatchIn("https://a.test/Ads/x")).isTrue()
        assertThat(regex.containsMatchIn("https://a.test/ads/x")).isFalse()
    }

    @Test
    fun `默认不区分大小写`() {
        val rule = (RuleParser.parseLine("||a.test/Ads/", 1) as ParseOutcome.Network).rule
        val regex = (rule.pattern as RulePattern.RegexPattern).regex
        assertThat(regex.containsMatchIn("https://a.test/ads/x")).isTrue()
    }

    @Test
    fun `空行与注释与列表头被忽略`() {
        assertThat(RuleParser.parseLine("   ", 1)).isInstanceOf(ParseOutcome.Ignored::class.java)
        assertThat(RuleParser.parseLine("! comment", 1)).isInstanceOf(ParseOutcome.Ignored::class.java)
        assertThat(RuleParser.parseLine("; comment", 1)).isInstanceOf(ParseOutcome.Ignored::class.java)
        assertThat(RuleParser.parseLine("# comment", 1)).isInstanceOf(ParseOutcome.Ignored::class.java)
        assertThat(RuleParser.parseLine("[Adblock Plus 2.0]", 1)).isInstanceOf(ParseOutcome.Ignored::class.java)
    }

    @Test
    fun `hosts 行多主机名时保留首个并给出警告`() {
        val outcome = RuleParser.parseLine("0.0.0.0 a.test b.test", 3, "src")
        val network = outcome as ParseOutcome.Network
        assertThat((network.rule.pattern as RulePattern.DomainOnly).domain).isEqualTo("a.test")
        assertThat(network.warnings).hasSize(1)
    }
}

/**
 * 移植到 Go 时发现的两处 Kotlin 侧缺陷的回归测试。
 *
 * 两处都由"跨语言语料一致性"暴露出来：Go 侧按同一份语料跑出
 * 元素规则 6 条 / 忽略行 35 行，而 Kotlin 侧原实现给出 7 / 34。
 * 修复后两侧必须给出同一组数字（另见 CorpusConsistencyTest 的计数断言）。
 */
class PortedDefectRegressionTest {

    @Test
    fun `起始与结尾锚定必须真的生效`() {
        // 修复前：结尾锚点被写成正则里的 `\$`（字面美元符），
        // 于是 `|...|` 形态的规则永远不可能命中。
        val outcome = RuleParser.parseLine("|https://anchor.test/x|", 1)
        val rule = (outcome as ParseOutcome.Network).rule
        val regex = (rule.pattern as RulePattern.RegexPattern).regex

        assertThat(regex.containsMatchIn("https://anchor.test/x")).isTrue()
        assertThat(regex.containsMatchIn("https://anchor.test/x?q=1")).isFalse()
        assertThat(regex.containsMatchIn("https://anchor.test/xy")).isFalse()
    }

    @Test
    fun `含双井号的注释行不得被误判为元素规则`() {
        // 修复前：注释正文里的 `##` 会被当成元素标记。
        val ignored = RuleParser.parseLine("# 说明（与 ## 元素规则区分）", 1)
        assertThat(ignored).isInstanceOf(ParseOutcome.Ignored::class.java)

        // 真正的全局元素规则仍必须被识别。
        val global = RuleParser.parseLine("##.ad-global", 1)
        assertThat(global).isInstanceOf(ParseOutcome.Cosmetic::class.java)
    }

    @Test
    fun `行数统计与 wc -l 一致`() {
        // 修复前用 lineSequence()：以换行结尾的文本会多算一个空行。
        assertThat(RuleParser.linesOf("a\n")).containsExactly("a")
        assertThat(RuleParser.linesOf("a")).containsExactly("a")
        assertThat(RuleParser.linesOf("a\nb\n")).containsExactly("a", "b").inOrder()
        // 三个换行符 → 三行：a 加两个空行（只去掉一个结尾空段）
        assertThat(RuleParser.linesOf("a\n\n\n")).containsExactly("a", "", "").inOrder()
        assertThat(RuleParser.linesOf("a\r\nb\r\n")).containsExactly("a", "b").inOrder()
        assertThat(RuleParser.linesOf("")).isEmpty()
    }
}
