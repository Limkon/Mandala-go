// 文件路径: android/app/src/main/java/com/example/mandala/data/NodeRepository.kt

package com.example.mandala.data

import android.content.Context
import com.example.mandala.viewmodel.Node
import com.example.mandala.viewmodel.Subscription // 需要在 ViewModel 中定义
import com.google.gson.Gson
import com.google.gson.reflect.TypeToken
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.File

class NodeRepository(private val context: Context) {
    private val gson = Gson()
    private val nodesFileName = "nodes.json"
    private val subsFileName = "subscriptions.json" // [新增] 订阅配置文件

    // 异步加载节点
    suspend fun loadNodes(): List<Node> = withContext(Dispatchers.IO) {
        val file = File(context.filesDir, nodesFileName)
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
            val file = File(context.filesDir, nodesFileName)
            file.writeText(json)
        } catch (e: Exception) {
            e.printStackTrace()
        }
    }

    // [新增] 异步加载订阅列表
    suspend fun loadSubscriptions(): List<Subscription> = withContext(Dispatchers.IO) {
        val file = File(context.filesDir, subsFileName)
        if (!file.exists()) return@withContext emptyList()

        try {
            val json = file.readText()
            val type = object : TypeToken<List<Subscription>>() {}.type
            gson.fromJson<List<Subscription>>(json, type) ?: emptyList()
        } catch (e: Exception) {
            e.printStackTrace()
            emptyList()
        }
    }

    // [新增] 异步保存订阅列表
    suspend fun saveSubscriptions(subs: List<Subscription>) = withContext(Dispatchers.IO) {
        try {
            val json = gson.toJson(subs)
            val file = File(context.filesDir, subsFileName)
            file.writeText(json)
        } catch (e: Exception) {
            e.printStackTrace()
        }
    }
}
