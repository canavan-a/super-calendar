package com.supercalendarapp

import com.facebook.react.ReactPackage

/**
 * What only the sideloaded build (distributed outside Google Play, see
 * release/release.sh) has: the in-app updater.
 */
object Flavor {
  fun packages(): List<ReactPackage> = listOf(AppUpdaterPackage())
}
