package com.zteonu.app

import android.os.Bundle
import android.text.InputType
import android.text.method.ScrollingMovementMethod
import android.view.WindowManager
import android.widget.ArrayAdapter
import android.widget.Button
import android.widget.CheckBox
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.Spinner
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.app.AppCompatActivity
import com.zteonu.core.zteonu.Logger
import com.zteonu.core.zteonu.Session
import com.zteonu.core.zteonu.Zteonu
import java.io.File
import kotlin.concurrent.thread

/**
 * MainActivity is a thin native UI over the gomobile-bound Go core
 * (com.zteonu.core). It mirrors the desktop app: one-click provisioning,
 * permanent telnet, and a small command console. All device work runs on the Go
 * side; progress is streamed back through the Logger callback.
 */
class MainActivity : AppCompatActivity() {

    private lateinit var col: LinearLayout
    private lateinit var logView: TextView

    private lateinit var ipEdit: EditText
    private lateinit var httpPortEdit: EditText
    private lateinit var telnetPortEdit: EditText
    private lateinit var facUserEdit: EditText
    private lateinit var facPassEdit: EditText
    private lateinit var macEdit: EditText

    private lateinit var snEdit: EditText
    private lateinit var passEdit: EditText
    private lateinit var ponSpinner: Spinner
    private lateinit var regionSpinner: Spinner
    private lateinit var reopenCheck: CheckBox

    private lateinit var modeSpinner: Spinner

    private lateinit var cmdEdit: EditText

    private var session: Session? = null
    @Volatile private var busy = false

    /** uiLogger forwards Go progress lines to the log view on the UI thread. */
    private val uiLogger = object : Logger {
        override fun log(line: String?) {
            val s = line ?: return
            runOnUiThread { appendLog(s) }
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        val scroll = ScrollView(this)
        col = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(16), dp(16), dp(16), dp(16))
        }
        scroll.addView(col)
        setContentView(scroll)

        // --- Connection ---
        addHeader("连接设置")
        ipEdit = addField("IP 地址", "192.168.1.1")
        httpPortEdit = addField("HTTP 端口", "80", number = true)
        telnetPortEdit = addField("telnet 端口", "23", number = true)
        facUserEdit = addField("工厂用户名", "CMCCAdmin")
        facPassEdit = addField("工厂密码", "aDm8H%MdA")
        macEdit = addField("自定义 MAC", "", hint = "如 00:07:29:55:35:57")
        addButton("尝试读取本机 MAC") { tryReadMac() }
        addNote("有线(USB转网口)接光猫：点上面按钮通常能读到网卡(eth0)MAC。WiFi 连接：安卓读不到，请到 设置→WLAN→当前网络 查看 MAC 并手动填。")

        // --- One-click ---
        addHeader("一键配置")
        snEdit = addField("SN", "", hint = "如 ZTEGXXXXXXXX，可留空")
        passEdit = addField("密码", "", hint = "认证/注册密码，可留空")
        ponSpinner = addSpinner("类型", listOf("GPON", "XGPON"))
        regionSpinner = addSpinner("区域", regionNames())
        regionSpinner.setSelection(Zteonu.defaultRegionIndex().toInt())
        reopenCheck = addCheckBox("完成后重新开启 telnet（便于重连）", true)
        addButton("一键配置") { onOneClick() }

        // --- Manual permanent telnet ---
        addHeader("手动")
        modeSpinner = addSpinner(
            "模式",
            listOf("仅打开临时 telnet", "永久 telnet（重启服务）", "永久 telnet（重启设备）")
        )
        addButton("运行") { onManual() }

        // --- Command console ---
        addHeader("命令")
        cmdEdit = addField("命令", "", hint = "如 sendcmd 1 DB show")
        val row = LinearLayout(this).apply { orientation = LinearLayout.HORIZONTAL }
        row.addView(Button(this).apply {
            text = "连接"
            layoutParams = LinearLayout.LayoutParams(0, wrap(), 1f)
            setOnClickListener { onConsoleConnect() }
        })
        row.addView(Button(this).apply {
            text = "断开"
            layoutParams = LinearLayout.LayoutParams(0, wrap(), 1f)
            setOnClickListener { onConsoleDisconnect() }
        })
        row.addView(Button(this).apply {
            text = "执行"
            layoutParams = LinearLayout.LayoutParams(0, wrap(), 1f)
            setOnClickListener { onConsoleSend() }
        })
        col.addView(row)

        // --- Log ---
        addHeader("日志输出")
        logView = TextView(this).apply {
            setTextIsSelectable(true)
            movementMethod = ScrollingMovementMethod()
            typeface = android.graphics.Typeface.MONOSPACE
            textSize = 12f
            minLines = 8
        }
        col.addView(logView)

        appendLog("就绪 - 填写参数后操作\r\n")
        autoFillMac()
    }

    // ---- actions ----

    private fun onOneClick() {
        if (!beginBusy()) return
        val ip = ipEdit.text.toString().trim()
        val httpPort = longOf(httpPortEdit, 80)
        val telnetPort = longOf(telnetPortEdit, 23)
        val facUser = facUserEdit.text.toString().trim()
        val facPass = facPassEdit.text.toString()
        val mac = macEdit.text.toString().trim()
        val sn = snEdit.text.toString().trim()
        val pass = passEdit.text.toString().trim()
        val xgpon = ponSpinner.selectedItemPosition == 1
        val regionID = Zteonu.regionIDAt(regionSpinner.selectedItemPosition.toLong())
        val reopen = reopenCheck.isChecked

        logView.text = ""
        thread {
            try {
                Zteonu.runOneClick(
                    ip, httpPort, telnetPort, facUser, facPass, mac,
                    sn, pass, xgpon, regionID, reopen, uiLogger
                )
            } catch (e: Exception) {
                runOnUiThread { appendLog("[错误] " + (e.message ?: e.toString()) + "\r\n") }
            } finally {
                runOnUiThread { endBusy() }
            }
        }
    }

    private fun onManual() {
        if (!beginBusy()) return
        val ip = ipEdit.text.toString().trim()
        val httpPort = longOf(httpPortEdit, 80)
        val telnetPort = longOf(telnetPortEdit, 23)
        val facUser = facUserEdit.text.toString().trim()
        val facPass = facPassEdit.text.toString()
        val mac = macEdit.text.toString().trim()
        val mode = modeSpinner.selectedItemPosition.toLong()

        logView.text = ""
        thread {
            try {
                Zteonu.openPermanentTelnet(ip, httpPort, telnetPort, facUser, facPass, mac, mode, uiLogger)
            } catch (e: Exception) {
                runOnUiThread { appendLog("[错误] " + (e.message ?: e.toString()) + "\r\n") }
            } finally {
                runOnUiThread { endBusy() }
            }
        }
    }

    private fun onConsoleConnect() {
        if (busy) return
        if (session != null) {
            appendLog("已连接\r\n"); return
        }
        val ip = ipEdit.text.toString().trim()
        val telnetPort = longOf(telnetPortEdit, 23)
        appendLog("正在连接 telnet $ip:$telnetPort …\r\n")
        thread {
            try {
                val s = Zteonu.connect(ip, telnetPort, "root", "Zte521")
                session = s
                runOnUiThread { appendLog("telnet 已连接\r\n") }
            } catch (e: Exception) {
                runOnUiThread { appendLog("[错误] 连接失败：" + (e.message ?: e.toString()) + "\r\n") }
            }
        }
    }

    private fun onConsoleDisconnect() {
        val s = session ?: return
        try {
            s.close()
        } catch (_: Exception) {
        }
        session = null
        appendLog("telnet 已断开\r\n")
    }

    private fun onConsoleSend() {
        val s = session
        if (s == null) {
            Toast.makeText(this, "请先点击“连接”", Toast.LENGTH_SHORT).show()
            return
        }
        val cmd = cmdEdit.text.toString().trim()
        if (cmd.isEmpty()) return
        appendLog("> $cmd\r\n")
        cmdEdit.setText("")
        thread {
            try {
                val out = s.exec(cmd)
                if (out.isNotBlank()) runOnUiThread { appendLog(out.trimEnd() + "\r\n") }
            } catch (e: Exception) {
                runOnUiThread {
                    appendLog("[错误] " + (e.message ?: e.toString()) + "\r\n")
                    try {
                        s.close()
                    } catch (_: Exception) {
                    }
                    session = null
                }
            }
        }
    }

    /** readableMacs scans /sys/class/net for interfaces with a usable MAC. This
     * file read works for USB/Ethernet adapters even when netlink route lookup
     * (auto-detection) is blocked on Android; Wi-Fi MACs are often still hidden. */
    private fun readableMacs(): List<Pair<String, String>> {
        val out = mutableListOf<Pair<String, String>>()
        val names = File("/sys/class/net").list()?.toList()
            ?: listOf("eth0", "eth1", "usb0", "rndis0", "wlan0")
        for (n in names) {
            if (n == "lo") continue
            try {
                val v = File("/sys/class/net/$n/address").readText().trim().lowercase()
                if (v.length == 17 && v != "00:00:00:00:00:00" && v != "02:00:00:00:00:00") {
                    out.add(n to v)
                }
            } catch (_: Exception) {
            }
        }
        // Prefer wired interfaces (the adapter to the ONU) over Wi-Fi.
        return out.sortedBy { (n, _) ->
            when {
                n.startsWith("eth") || n.startsWith("usb") || n.startsWith("rndis") || n.startsWith("en") -> 0
                n.startsWith("wlan") -> 2
                else -> 1
            }
        }
    }

    /** autoFillMac pre-fills the MAC field on launch if a wired MAC is readable. */
    private fun autoFillMac() {
        if (macEdit.text.isNotBlank()) return
        val macs = readableMacs()
        val best = macs.firstOrNull() ?: return
        macEdit.setText(best.second)
        appendLog("已自动填入 ${best.first} 的 MAC：${best.second}\r\n")
    }

    /** tryReadMac fills the MAC field from the best readable interface, or tells
     * the user to enter it manually. */
    private fun tryReadMac() {
        val macs = readableMacs()
        if (macs.isEmpty()) {
            Toast.makeText(this, "读不到网卡 MAC，请手动填写适配器/本机 MAC", Toast.LENGTH_LONG).show()
            return
        }
        val best = macs.first()
        macEdit.setText(best.second)
        val others = if (macs.size > 1) "（其它：" + macs.drop(1).joinToString("，") { "${it.first} ${it.second}" } + "）" else ""
        Toast.makeText(this, "读到 ${best.first}: ${best.second} $others", Toast.LENGTH_LONG).show()
        appendLog("网卡 MAC：" + macs.joinToString("，") { "${it.first}=${it.second}" } + "\r\n")
    }

    // ---- helpers ----

    private fun beginBusy(): Boolean {
        if (busy) return false
        busy = true
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        return true
    }

    private fun endBusy() {
        busy = false
        window.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
    }

    private fun appendLog(s: String) {
        logView.append(s.replace("\r\n", "\n"))
        // autoscroll
        (logView.parent as? ScrollView)?.post { }
    }

    private fun regionNames(): List<String> {
        val n = Zteonu.regionCount().toInt()
        return (0 until n).map { Zteonu.regionNameAt(it.toLong()) }
    }

    private fun addHeader(text: String) {
        col.addView(TextView(this).apply {
            this.text = text
            setPadding(0, dp(14), 0, dp(4))
            textSize = 15f
            setTypeface(typeface, android.graphics.Typeface.BOLD)
        })
    }

    private fun addNote(text: String) {
        col.addView(TextView(this).apply {
            this.text = text
            textSize = 12f
            setTextColor(0xFF666666.toInt())
        })
    }

    private fun addField(label: String, default: String, number: Boolean = false, hint: String = ""): EditText {
        col.addView(TextView(this).apply { this.text = label; textSize = 13f })
        val e = EditText(this).apply {
            setText(default)
            this.hint = hint
            if (number) inputType = InputType.TYPE_CLASS_NUMBER
        }
        col.addView(e)
        return e
    }

    private fun addSpinner(label: String, items: List<String>): Spinner {
        col.addView(TextView(this).apply { this.text = label; textSize = 13f })
        val sp = Spinner(this)
        sp.adapter = ArrayAdapter(this, android.R.layout.simple_spinner_dropdown_item, items)
        col.addView(sp)
        return sp
    }

    private fun addCheckBox(text: String, checked: Boolean): CheckBox {
        val cb = CheckBox(this).apply { this.text = text; isChecked = checked }
        col.addView(cb)
        return cb
    }

    private fun addButton(text: String, onClick: () -> Unit): Button {
        val b = Button(this).apply {
            this.text = text
            setOnClickListener { onClick() }
        }
        col.addView(b)
        return b
    }

    private fun longOf(e: EditText, def: Long): Long =
        e.text.toString().trim().toLongOrNull()?.takeIf { it > 0 } ?: def

    private fun dp(v: Int): Int = (v * resources.displayMetrics.density).toInt()
    private fun wrap(): Int = LinearLayout.LayoutParams.WRAP_CONTENT
}
