package com.example.mandala.utils

import android.net.Uri
import android.util.Base64
import com.example.mandala.viewmodel.Node
import com.google.gson.Gson
import com.google.gson.JsonObject
import java.nio.charset.StandardCharsets

object NodeParser {
    fun parse(link: String): Node? {
        val trimmed = link.trim()
        return try {
            when {
                trimmed.startsWith("vmess://") -> parseVmess(trimmed)
                trimmed.startsWith("trojan://") -> parseTrojan(trimmed)
                trimmed.startsWith("mandala://") -> parseMandala(trimmed)
                else -> null
            }
        } catch (e: Exception) { null }
    }

    private fun parseVmess(link: String): Node? {
        val base64Str = link.removePrefix("vmess://")
        val decoded = try { Base64.decode(base64Str, Base64.DEFAULT) } 
                      catch (e: Exception) { Base64.decode(base64Str, Base64.URL_SAFE) }
        val json = Gson().fromJson(String(decoded, StandardCharsets.UTF_8), JsonObject::class.java)

        return Node(
            tag = json.get("ps")?.asString ?: "Vmess",
            protocol = "vless",
            server = json.get("add")?.asString ?: return null,
            port = json.get("port")?.asString?.toIntOrNull() ?: 443,
            uuid = json.get("id")?.asString ?: "",
            transport = if (json.get("net")?.asString == "ws") "ws" else "tcp",
            path = json.get("path")?.asString ?: "/",
            sni = json.get("sni")?.asString ?: ""
        )
    }

    private fun parseTrojan(link: String): Node? {
        val uri = Uri.parse(link)
        return Node(
            tag = uri.fragment ?: "Trojan",
            protocol = "trojan",
            server = uri.host ?: return null,
            port = if (uri.port > 0) uri.port else 443,
            password = uri.userInfo ?: "",
            transport = if (uri.getQueryParameter("type") == "ws") "ws" else "tcp",
            path = uri.getQueryParameter("path") ?: "/",
            sni = uri.getQueryParameter("sni") ?: uri.getQueryParameter("peer") ?: ""
        )
    }

    private fun parseMandala(link: String): Node? {
        val uri = Uri.parse(link)
        return Node(
            tag = uri.fragment ?: "Mandala",
            protocol = "mandala",
            server = uri.host ?: return null,
            port = if (uri.port > 0) uri.port else 443,
            password = uri.userInfo ?: "",
            uuid = uri.getQueryParameter("uuid") ?: "",
            transport = uri.getQueryParameter("transport") ?: "tcp",
            path = uri.getQueryParameter("path") ?: "/"
        )
    }
}
