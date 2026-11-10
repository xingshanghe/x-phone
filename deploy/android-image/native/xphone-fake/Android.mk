LOCAL_PATH := $(call my-dir)

include $(CLEAR_VARS)
LOCAL_MODULE            := xphone_fake
LOCAL_SRC_FILES         := xphone_fake.c
LOCAL_CFLAGS            := -std=c11 -O2 -fPIC -Wall
LOCAL_LDFLAGS           := -ldl
LOCAL_MODULE_PATH       := $(TARGET_OUT)/../lib64
include $(BUILD_SHARED_LIBRARY)
