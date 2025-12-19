// 文件路径: android/app/src/main/java/com/example/mandala/utils/NodeParser.kt

package com.example.mandala.utils

import android.net.Uri
import android.util.Base64
import com.example.mandala.viewmodel.Node
import com.google.gson.Gson
import com.google.gson.JsonObject
import java.nio.charset.StandardCharsets

object NodeParser {
    fun parse(link: String): Node? {
        // 清理不可见字符和换行
        val trimmed = link.trim().replace("\n", "").replace("\r", "")
        return try {
            when {
                trimmed.startsWith("vmess://", ignoreCase = true) -> parseVmess(trimmed)
                trimmed.startsWith("vless://", ignoreCase = true) -> parseVless(trimmed)
                trimmed.startsWith("trojan://", ignoreCase = true) -> parseTrojan(trimmed)
                else -> null
            }
        } catch (e: Exception) {
            e.printStackTrace()
            null
        }
    }

    private fun parseVmess(link: String): Node? {
        // 移除前缀并截断可能的查询参数（如 ?remark=...）
        var base64Part = link.substring(8).trim()
        if (base64Part.contains("?")) {
            base64Part = base64Part.substringBefore("?")
        }

        // 使用最宽松的 Base64 标志位尝试解码
        val decodedBytes = try {
            Base64.decode(base64Part, Base64.URL_SAFE or Base64.NO_WRAP or Base64.NO_PADDING)
        } catch (e: Exception) {
            try {
                Base64.decode(base64Part, Base64.DEFAULT)
            } catch (e2: Exception) {
                return null
            }
        }

        val jsonStr = String(decodedBytes, StandardCharsets.UTF_8)
        val json = Gson().fromJson(jsonStr, JsonObject::class.java)

        // 提取端口（处理 Int 和 String 两种可能）
        val portElement = json.get("port")
        val port = when {
            portElement == null -> 443
            portElement.isJsonPrimitive && portElement.asJsonPrimitive.isNumber -> portElement.asInt
            else -> portElement.asString.toIntOrNull() ?: 443
        }

        return Node(
            tag = json.get("ps")?.asString?.let { Uri.decode(it) } ?: "未命名VMess",
            protocol = "vless", // 映射至 Go 核心的 vless 分支逻辑
            server = json.get("add")?.asString ?: return null,
            port = port,
            uuid = json.get("id")?.asString ?: "",
            transport = if (json.get("net")?.asString == "ws") "ws" else "tcp",
            path = json.get("path")?.asString ?: "/",
            sni = json.get("sni")?.asString ?: ""
        )
    }

    private fun parseTrojan(link: String): Node? {
        val uri = Uri.parse(link)
        val host = uri.host ?: return null
        
        return Node(
            tag = uri.fragment?.let { Uri.decode(it) } ?: "未命名Trojan",
            protocol = "trojan",
            server = host,
            port = if (uri.port > 0) uri.port else 443,
            password = uri.userInfo?.let { Uri.decode(it) } ?: "",
            transport = if (uri.getQueryParameter("type") == "ws") "ws" else "tcp",
            path = uri.getQueryParameter("path") ?: "/",
            sni = uri.getQueryParameter("sni") ?: ""
        )
    }

    private fun parseVless(link: String): Node? {
        val uri = Uri.parse(link)
        val host = uri.host ?: return null

        return Node(
            tag = uri.fragment?.let { Uri.decode(it) } ?: "未命名VLESS",
            protocol = "vless",
            server = host,
            port = if (uri.port > 0) uri.port else 443,
            uuid = uri.userInfo?.let { Uri.decode(it) } ?: "",
            transport = if (uri.getQueryParameter("type") == "ws") "ws" else "tcp",
            path = uri.getQueryParameter("path") ?: "/",
            sni = uri.getQueryParameter("sni") ?: ""
        )
    }
}
