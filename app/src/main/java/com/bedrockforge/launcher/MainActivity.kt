package com.bedrockforge.launcher

import android.app.Activity
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.provider.OpenableColumns
import android.widget.*
import androidx.appcompat.app.AppCompatActivity
import com.google.gson.Gson
import java.io.*

class MainActivity : AppCompatActivity() {

    private lateinit var tvStatus: TextView
    private lateinit var tvAuth: TextView
    private lateinit var tvLog: TextView
    private lateinit var etServer: EditText
    private lateinit var btnStartStop: Button
    private lateinit var btnAuth: Button
    private lateinit var btnAddSchem: Button
    private lateinit var lvSchematics: ListView
    private lateinit var scrollLog: ScrollView

    private var proxyProcess: Process? = null
    private var running = false
    private val handler = Handler(Looper.getMainLooper())
    private val schemFiles = mutableListOf<String>()
    private lateinit var schemAdapter: ArrayAdapter<String>

    private val PICK_SCHEM = 1001
    private val AUTH_REQUEST = 1002

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        tvStatus = findViewById(R.id.tvStatus)
        tvAuth = findViewById(R.id.tvAuth)
        tvLog = findViewById(R.id.tvLog)
        etServer = findViewById(R.id.etServer)
        btnStartStop = findViewById(R.id.btnStartStop)
        btnAuth = findViewById(R.id.btnAuth)
        btnAddSchem = findViewById(R.id.btnAddSchem)
        lvSchematics = findViewById(R.id.lvSchematics)
        scrollLog = findViewById(R.id.scrollLog)

        // Load saved server
        etServer.setText(getPreferences(MODE_PRIVATE).getString("server", ""))

        // Setup schematic list
        schemAdapter = ArrayAdapter(this, android.R.layout.simple_list_item_1, schemFiles)
        lvSchematics.adapter = schemAdapter
        refreshSchemList()

        // Long press to delete schematic
        lvSchematics.setOnItemLongClickListener { _, _, pos, _ ->
            val name = schemFiles[pos]
            File(schemDir(), name).delete()
            refreshSchemList()
            Toast.makeText(this, "Deleted $name", Toast.LENGTH_SHORT).show()
            true
        }

        // Check auth
        checkAuthStatus()

        btnAuth.setOnClickListener {
            startActivityForResult(Intent(this, AuthActivity::class.java), AUTH_REQUEST)
        }

        btnAddSchem.setOnClickListener {
            val intent = Intent(Intent.ACTION_GET_CONTENT)
            intent.type = "*/*"
            intent.putExtra(Intent.EXTRA_MIME_TYPES, arrayOf(
                "application/octet-stream", "*/*"
            ))
            startActivityForResult(intent, PICK_SCHEM)
        }

        btnStartStop.setOnClickListener {
            if (running) stopProxy() else startProxy()
        }
    }

    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        when (requestCode) {
            PICK_SCHEM -> {
                if (resultCode == Activity.RESULT_OK) {
                    data?.data?.let { importSchematic(it) }
                }
            }
            AUTH_REQUEST -> {
                checkAuthStatus()
            }
        }
    }

    private fun importSchematic(uri: Uri) {
        val name = getFileName(uri) ?: "unknown.litematic"
        val valid = listOf(".litematic", ".schem", ".schematic", ".nbt", ".json")
        if (valid.none { name.endsWith(it) }) {
            Toast.makeText(this, "Unsupported file type", Toast.LENGTH_SHORT).show()
            return
        }
        val dest = File(schemDir(), name)
        contentResolver.openInputStream(uri)?.use { input ->
            dest.outputStream().use { output -> input.copyTo(output) }
        }
        refreshSchemList()
        Toast.makeText(this, "Imported $name", Toast.LENGTH_SHORT).show()
    }

    private fun getFileName(uri: Uri): String? {
        contentResolver.query(uri, null, null, null, null)?.use { cursor ->
            val idx = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME)
            if (cursor.moveToFirst() && idx >= 0) return cursor.getString(idx)
        }
        return uri.lastPathSegment
    }

    private fun schemDir(): File {
        val dir = File(filesDir, "schematics")
        dir.mkdirs()
        return dir
    }

    private fun refreshSchemList() {
        schemFiles.clear()
        schemDir().listFiles()?.forEach { schemFiles.add(it.name) }
        schemAdapter.notifyDataSetChanged()
    }

    private fun checkAuthStatus() {
        val token = File(filesDir, "token.json")
        if (token.exists()) {
            tvAuth.text = "Xbox: ✓ Logged in"
            tvAuth.setTextColor(0xFF1DB954.toInt())
            btnAuth.text = "Re-login"
        } else {
            tvAuth.text = "Xbox: Not logged in"
            tvAuth.setTextColor(0xFFAAAAAA.toInt())
            btnAuth.text = "Login"
        }
    }

    private fun extractBinary(): File {
        val bin = File(filesDir, "mcproxy")
        if (!bin.exists() || bin.length() == 0L) {
            assets.open("bedrockforge-android").use { input ->
                bin.outputStream().use { output -> input.copyTo(output) }
            }
            bin.setExecutable(true)
        }
        return bin
    }

    private fun extractAsset(name: String): File {
        val dest = File(filesDir, name)
        if (!dest.exists()) {
            assets.open(name).use { input ->
                dest.outputStream().use { output -> input.copyTo(output) }
            }
        }
        return dest
    }

    private fun startProxy() {
        val server = etServer.text.toString().trim()
        if (server.isEmpty()) {
            Toast.makeText(this, "Enter server address first", Toast.LENGTH_SHORT).show()
            return
        }
        if (!File(filesDir, "token.json").exists()) {
            Toast.makeText(this, "Login to Xbox first", Toast.LENGTH_SHORT).show()
            return
        }

        // Save server
        getPreferences(MODE_PRIVATE).edit().putString("server", server).apply()

        // Extract binary and assets
        val bin = extractBinary()
        extractAsset("bedrock_blocks.json")

        // Write config.json
        val config = mapOf("server" to server, "listen" to "0.0.0.0:19132")
        File(filesDir, "config.json").writeText(Gson().toJson(config))

        try {
            val pb = ProcessBuilder(bin.absolutePath)
            pb.directory(filesDir)
            pb.redirectErrorStream(true)
            pb.environment()["HOME"] = filesDir.absolutePath

            proxyProcess = pb.start()
            running = true

            tvStatus.text = "● Running"
            tvStatus.setTextColor(0xFF1DB954.toInt())
            btnStartStop.text = "STOP PROXY"
            btnStartStop.backgroundTintList = android.content.res.ColorStateList.valueOf(0xFFCC4444.toInt())

            appendLog("Started proxy -> $server\n")
            appendLog("Connect Minecraft to: your-ip:19132\n")

            // Read output
            Thread {
                val reader = BufferedReader(InputStreamReader(proxyProcess!!.inputStream))
                var line: String?
                while (reader.readLine().also { line = it } != null) {
                    val l = line!!
                    handler.post { appendLog(l + "\n") }
                }
                handler.post {
                    if (running) {
                        running = false
                        tvStatus.text = "● Stopped"
                        tvStatus.setTextColor(0xFFFF4444.toInt())
                        btnStartStop.text = "START PROXY"
                        btnStartStop.backgroundTintList = android.content.res.ColorStateList.valueOf(0xFF1DB954.toInt())
                        appendLog("Proxy stopped.\n")
                    }
                }
            }.start()

        } catch (e: Exception) {
            appendLog("Error: ${e.message}\n")
        }
    }

    private fun stopProxy() {
        proxyProcess?.destroy()
        proxyProcess = null
        running = false
        tvStatus.text = "● Stopped"
        tvStatus.setTextColor(0xFFFF4444.toInt())
        btnStartStop.text = "START PROXY"
        btnStartStop.backgroundTintList = android.content.res.ColorStateList.valueOf(0xFF1DB954.toInt())
        appendLog("Proxy stopped.\n")
    }

    private fun appendLog(text: String) {
        tvLog.append(text)
        scrollLog.post { scrollLog.fullScroll(ScrollView.FOCUS_DOWN) }
        // Keep log under 200 lines
        val lines = tvLog.text.split("\n")
        if (lines.size > 200) {
            tvLog.text = lines.takeLast(200).joinToString("\n")
        }
    }

    override fun onDestroy() {
        super.onDestroy()
        stopProxy()
    }
}
