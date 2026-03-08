package com.bedrockforge.launcher

import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.widget.Button
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import java.io.*
import java.net.HttpURLConnection
import java.net.URL

class AuthActivity : AppCompatActivity() {

    private lateinit var tvAuthUrl: TextView
    private lateinit var tvAuthStatus: TextView
    private lateinit var btnOpenBrowser: Button
    private var authUrl = ""
    private var authProcess: Process? = null
    private val handler = Handler(Looper.getMainLooper())

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_auth)

        tvAuthUrl = findViewById(R.id.tvAuthUrl)
        tvAuthStatus = findViewById(R.id.tvAuthStatus)
        btnOpenBrowser = findViewById(R.id.btnOpenBrowser)

        btnOpenBrowser.setOnClickListener {
            if (authUrl.isNotEmpty()) {
                startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(authUrl)))
            }
        }

        startAuth()
    }

    private fun startAuth() {
        val bin = File(filesDir, "mcproxy")
        if (!bin.exists()) {
            assets.open("bedrockforge-android").use { input ->
                bin.outputStream().use { output -> input.copyTo(output) }
            }
            bin.setExecutable(true)
        }

        // Delete existing token to force re-auth
        File(filesDir, "token.json").delete()

        Thread {
            try {
                val pb = ProcessBuilder(bin.absolutePath, "--auth-only")
                pb.directory(filesDir)
                pb.redirectErrorStream(true)
                pb.environment()["HOME"] = filesDir.absolutePath
                authProcess = pb.start()

                val reader = BufferedReader(InputStreamReader(authProcess!!.inputStream))
                var line: String?
                while (reader.readLine().also { line = it } != null) {
                    val l = line!!
                    // Look for the auth URL in output
                    if (l.contains("https://") && (l.contains("login") || l.contains("microsoft") || l.contains("live"))) {
                        val urlStart = l.indexOf("https://")
                        authUrl = l.substring(urlStart).trim()
                        handler.post {
                            tvAuthUrl.text = "Open this URL in your browser:\n\n$authUrl"
                            btnOpenBrowser.isEnabled = true
                        }
                    }
                    if (l.contains("token saved") || l.contains("authenticated")) {
                        handler.post {
                            tvAuthStatus.text = "✓ Login successful!"
                            tvAuthStatus.setTextColor(0xFF1DB954.toInt())
                        }
                        Thread.sleep(1500)
                        handler.post { finish() }
                    }
                }
            } catch (e: Exception) {
                handler.post {
                    tvAuthStatus.text = "Error: ${e.message}"
                }
            }
        }.start()
    }

    override fun onDestroy() {
        super.onDestroy()
        authProcess?.destroy()
    }
}
