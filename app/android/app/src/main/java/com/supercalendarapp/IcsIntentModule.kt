package com.supercalendarapp

import android.app.Activity
import android.content.Intent
import android.net.Uri
import android.os.Build
import com.facebook.react.ReactPackage
import com.facebook.react.bridge.ActivityEventListener
import com.facebook.react.bridge.Arguments
import com.facebook.react.bridge.NativeModule
import com.facebook.react.bridge.Promise
import com.facebook.react.bridge.ReactApplicationContext
import com.facebook.react.bridge.ReactContextBaseJavaModule
import com.facebook.react.bridge.ReactMethod
import com.facebook.react.bridge.WritableMap
import com.facebook.react.modules.core.DeviceEventManagerModule
import com.facebook.react.uimanager.ViewManager

/**
 * The ways a calendar file gets into the app (src/icsIntents.ts), since
 * Android has no drag-and-drop from websites:
 *
 *  - tapping an .ics or webcal:// link (VIEW intents, AndroidManifest.xml),
 *  - "Share → Super Calendar" with a file or a link (SEND),
 *  - "Import file" (the system file picker, pickFile).
 *
 * Each gives JS {text} (the file's contents) or {url} (for the server to
 * fetch). The launch intent is read once with getInitial; later ones arrive
 * as "SuperCalendarIcs" events.
 */
class IcsIntentModule(private val context: ReactApplicationContext) :
    ReactContextBaseJavaModule(context), ActivityEventListener {

  private var pickPromise: Promise? = null
  private var initialHandled = false

  init {
    context.addActivityEventListener(this)
  }

  override fun getName() = "IcsIntent"

  @ReactMethod
  fun getInitial(promise: Promise) {
    if (initialHandled) {
      promise.resolve(null)
      return
    }
    initialHandled = true
    val intent = context.currentActivity?.intent
    promise.resolve(intent?.let { read(it) })
  }

  @ReactMethod
  fun pickFile(promise: Promise) {
    val activity = context.currentActivity
    if (activity == null) {
      promise.reject("no_activity", "The app isn't in front")
      return
    }
    pickPromise?.resolve(null)
    pickPromise = promise
    val intent = Intent(Intent.ACTION_OPEN_DOCUMENT).apply {
      addCategory(Intent.CATEGORY_OPENABLE)
      // Many file managers label .ics files octet-stream, so allow anything
      // and let the server say if it isn't a calendar.
      type = "*/*"
      putExtra(Intent.EXTRA_MIME_TYPES, arrayOf("text/calendar", "application/ics", "text/x-vcalendar", "application/octet-stream", "text/plain"))
    }
    try {
      activity.startActivityForResult(intent, PICK_REQUEST)
    } catch (e: Exception) {
      pickPromise = null
      promise.reject("no_picker", "There's no file picker on this phone")
    }
  }

  // Required by NativeEventEmitter on the JS side.
  @ReactMethod fun addListener(eventName: String) {}
  @ReactMethod fun removeListeners(count: Double) {}

  override fun onActivityResult(activity: Activity, requestCode: Int, resultCode: Int, data: Intent?) {
    if (requestCode != PICK_REQUEST) return
    val promise = pickPromise ?: return
    pickPromise = null
    val uri = data?.data
    if (resultCode != Activity.RESULT_OK || uri == null) {
      promise.resolve(null)
      return
    }
    try {
      promise.resolve(Arguments.createMap().apply { putString("text", readText(uri)) })
    } catch (e: Exception) {
      promise.reject("read_failed", e.message ?: "Couldn't read that file")
    }
  }

  override fun onNewIntent(intent: Intent) {
    val payload = read(intent) ?: return
    context
        .getJSModule(DeviceEventManagerModule.RCTDeviceEventEmitter::class.java)
        .emit("SuperCalendarIcs", payload)
  }

  private fun read(intent: Intent): WritableMap? {
    return try {
      when (intent.action) {
        Intent.ACTION_VIEW -> intent.data?.let { fromUri(it) }
        Intent.ACTION_SEND -> {
          val stream: Uri? =
              if (Build.VERSION.SDK_INT >= 33) intent.getParcelableExtra(Intent.EXTRA_STREAM, Uri::class.java)
              else @Suppress("DEPRECATION") intent.getParcelableExtra(Intent.EXTRA_STREAM)
          if (stream != null) fromUri(stream)
          else intent.getStringExtra(Intent.EXTRA_TEXT)?.let { fromText(it) }
        }
        else -> null
      }
    } catch (e: Exception) {
      Arguments.createMap().apply { putString("error", e.message ?: "Couldn't read that calendar") }
    }
  }

  private fun fromUri(uri: Uri): WritableMap? {
    return when (uri.scheme?.lowercase()) {
      "webcal", "webcals", "http", "https" -> Arguments.createMap().apply { putString("url", uri.toString()) }
      "content", "file" -> Arguments.createMap().apply { putString("text", readText(uri)) }
      else -> null
    }
  }

  private fun fromText(text: String): WritableMap? {
    if (text.contains("BEGIN:VCALENDAR")) return Arguments.createMap().apply { putString("text", text) }
    val link = Regex("""(webcals?|https?)://\S+""").find(text)?.value ?: return null
    return Arguments.createMap().apply { putString("url", link) }
  }

  private fun readText(uri: Uri): String {
    val input = context.contentResolver.openInputStream(uri) ?: throw IllegalStateException("Couldn't open that file")
    input.use {
      val bytes = it.readNBytesCompat(MAX_BYTES + 1)
      if (bytes.size > MAX_BYTES) throw IllegalStateException("That file is over 5 MB")
      return String(bytes, Charsets.UTF_8)
    }
  }

  private fun java.io.InputStream.readNBytesCompat(limit: Int): ByteArray {
    val out = java.io.ByteArrayOutputStream()
    val buf = ByteArray(16 * 1024)
    while (out.size() < limit) {
      val n = read(buf, 0, minOf(buf.size, limit - out.size()))
      if (n < 0) break
      out.write(buf, 0, n)
    }
    return out.toByteArray()
  }

  companion object {
    private const val PICK_REQUEST = 4711
    private const val MAX_BYTES = 5 * 1024 * 1024
  }
}

class IcsIntentPackage : ReactPackage {
  override fun createNativeModules(context: ReactApplicationContext): List<NativeModule> =
      listOf(IcsIntentModule(context))

  override fun createViewManagers(context: ReactApplicationContext): List<ViewManager<*, *>> = emptyList()
}
