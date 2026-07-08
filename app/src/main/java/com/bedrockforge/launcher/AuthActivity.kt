package com.bedrockforge.launcher

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.widget.Button
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.app.AppCompatActivity
import java.io.*

class AuthActivity : AppCompatActivity() {

    private lateinit var tvAuthUrl: TextView
    private lateinit var tvAuthCode: TextView
    private lateinit var tvAuthStatus: TextView
    private lateinit var btnOpenBrowser: Button
    private lateinit var btnCopyCode: Button
    private var authUrl = ""
    private var authCode = ""
    private var authProcess: Process? = null
    private val handler = Handler(Looper.getMainLooper())

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_auth)

        tvAuthUrl = findViewById(R.id.tvAuthUrl)
        tvAuthCode = findViewById(R.id.tvAuthCode)
        tvAuthStatus = findViewById(R.id.tvAuthStatus)
        btnOpenBrowser = findViewById(R.id.btnOpenBrowser)
        btnCopyCode = findViewById(R.id.btnCopyCode)

        btnOpenBrowser.isEnabled = false
        btnCopyCode.isEnabled = false

        btnOpenBrowser.setOnClickListener {
            if (authUrl.isNotEmpty()) {
                startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(authUrl)))
            }
        }
        btnCopyCode.setOnClickListener {
            if (authCode.isNotEmpty()) {
                val cb = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
                cb.setPrimaryClip(ClipData.newPlainText("code", authCode))
                Toast.makeText(this, "Code copied", Toast.LENGTH_SHORT).show()
            }
        }

        startAuth()
    }

    private fun startAuth() {
        val bin = File(applicationInfo.nativeLibraryDir, "libmcproxy.so")
        if (!bin.exists()) {
            tvAuthStatus.text = "Error: proxy binary missing from APK"
            return
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
                    val l = line!!.trim()
                    when {
                        l.startsWith("AUTH_URL ") -> {
                            authUrl = l.removePrefix("AUTH_URL ").trim()
                            handler.post {
                                tvAuthUrl.text = authUrl
                                btnOpenBrowser.isEnabled = true
                            }
                        }
                        l.startsWith("AUTH_CODE ") -> {
                            authCode = l.removePrefix("AUTH_CODE ").trim()
                            handler.post {
                                tvAuthCode.text = authCode
                                btnCopyCode.isEnabled = true
                                tvAuthStatus.text = "Enter this code on the Microsoft page"
                            }
                        }
                        l.contains("token saved") || l.contains("Authentication successful") -> {
                            handler.post {
                                tvAuthStatus.text = "✓ Login successful!"
                                tvAuthStatus.setTextColor(0xFF1DB954.toInt())
                            }
                            Thread.sleep(1500)
                            handler.post {
                                setResult(RESULT_OK)
                                finish()
                            }
                        }
                        l.startsWith("auth failed") || l.contains("error polling") -> {
                            handler.post { tvAuthStatus.text = "Login failed: $l" }
                        }
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
