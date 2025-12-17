// 文件路径: android/app/src/main/java/com/example/mandala/data/NodeRepository.kt

package com.example.mandala.data

import android.content.Context
import com.example.mandala.viewmodel.Node
import com.google.gson.Gson
import com.google.gson.reflect.TypeToken
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.File

class NodeRepository(private val context: Context) {
    private val gson = Gson()
    private val fileName = "nodes.json"

    // 异步加载节点
    suspend fun loadNodes(): List<Node> = withContext(Dispatchers.IO) {
        val file = File(context.filesDir, fileName)
        if (!file.exists()) return@withContext emptyList()

        try {
            val json = file.readText()
            val type = object : TypeToken<List<Node>>() {}.type
            gson.fromJson<List<Node>>(json, type) ?: emptyList()
        } catch (e: Exception) {
            e.printStackTrace()
            emptyList()
        }
    }

    // 异步保存节点
    suspend fun saveNodes(nodes: List<Node>) = withContext(Dispatchers.IO) {
        try {
            val json = gson.toJson(nodes)
            val file = File(context.filesDir, fileName)
            file.writeText(json)
        } catch (e: Exception) {
            e.printStackTrace()
        }
    }
}