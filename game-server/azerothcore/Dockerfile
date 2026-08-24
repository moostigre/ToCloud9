FROM ubuntu:22.04 AS build-sidecar

ENV DEBIAN_FRONTEND=noninteractive

WORKDIR /build

# Install build dependencies for libsidecar-cpp
RUN apt-get update && \
    apt-get install -y \
        git \
        cmake \
        make \
        gcc \
        g++ \
        build-essential \
        libgrpc++-dev \
        protobuf-compiler \
        protobuf-compiler-grpc \
        libssl-dev \
        zlib1g-dev \
        ca-certificates \
        pkg-config && \
    rm -rf /var/lib/apt/lists/*

# Copy source files (proto must be at /api/proto for CMake path resolution)
COPY api/proto /api/proto
COPY game-server/libsidecar-cpp /build/libsidecar-cpp

# Build libsidecar-cpp from a clean CMake tree.
WORKDIR /build/libsidecar-cpp
RUN rm -rf build && mkdir build && cd build && \
    cmake .. \
        -DCMAKE_BUILD_TYPE=Release \
        -DBUILD_TESTS=OFF \
        -DUSE_SYSTEM_GRPC=ON \
        -DUSE_SYSTEM_PROTOBUF=ON && \
    cmake --build . --config Release -j $(nproc)

FROM ubuntu:22.04 AS build-core

ENV DEBIAN_FRONTEND=noninteractive

# runs from project directory
WORKDIR /

RUN apt-get update || true && \
    apt-get install -y --reinstall ubuntu-keyring && \
    apt-get update

RUN apt-get install -yq git clang cmake make gcc g++ libmysqlclient-dev libssl-dev libbz2-dev libreadline-dev libncurses-dev libboost-all-dev libgrpc++-dev libprotobuf-dev mysql-server p7zip
RUN update-alternatives --install /usr/bin/cc cc /usr/bin/clang 100
RUN update-alternatives --install /usr/bin/c++ c++ /usr/bin/clang 100

# Trusted CI supplies an exact commit from the official AzerothCore repository.
# Keeping the repository fixed prevents build requests from turning this image
# into a generic remote-code fetcher.
ARG AC_REPOSITORY=https://github.com/azerothcore/azerothcore-wotlk.git
ARG AC_REF=ca8e6d78dee182fbe4c840dbc24a577c25844593

RUN mkdir repo
RUN cd repo && git init && \
    test "$AC_REPOSITORY" = "https://github.com/azerothcore/azerothcore-wotlk.git" && \
    git remote add origin "$AC_REPOSITORY" && \
    git fetch --depth 1 origin "$AC_REF" && \
    git checkout FETCH_HEAD

# Guard reinforcement system from the pinned AzerothCore bugfix branch.
RUN cd /repo && \
    git fetch --depth 4 https://github.com/moostigre/azerothcore-wotlk.git ad10279469940a679ccebd0ce9de9e9ee6f87949 && \
    git diff --binary FETCH_HEAD~3 FETCH_HEAD -- . ':(exclude)data/sql/updates/db_world/2026_08_16_00.sql' | git apply --3way

COPY game-server/azerothcore/patches /tmp/azerothcore-patches
RUN cd /repo && \
    for patch in /tmp/azerothcore-patches/*.patch; do \
        git apply --check --unidiff-zero "$patch" && \
        git apply --unidiff-zero "$patch"; \
    done

# SWP modules are staged from their pinned repositories before the image build.
COPY --from=build-sidecar /build/libsidecar-cpp/build/libsidecar.so ./repo/deps/libsidecar/libsidecar.so
COPY --from=build-sidecar /build/libsidecar-cpp/include/libsidecar.h ./repo/deps/libsidecar/include/libsidecar.h
COPY --from=build-sidecar /build/libsidecar-cpp/include/events-guild.h ./repo/deps/libsidecar/include/events-guild.h
COPY --from=build-sidecar /build/libsidecar-cpp/include/events-group.h ./repo/deps/libsidecar/include/events-group.h
COPY --from=build-sidecar /build/libsidecar-cpp/include/events-servers-registry.h ./repo/deps/libsidecar/include/events-servers-registry.h
COPY --from=build-sidecar /build/libsidecar-cpp/include/player-items-api.h ./repo/deps/libsidecar/include/player-items-api.h
COPY --from=build-sidecar /build/libsidecar-cpp/include/player-money-api.h ./repo/deps/libsidecar/include/player-money-api.h
COPY --from=build-sidecar /build/libsidecar-cpp/include/player-interactions-api.h ./repo/deps/libsidecar/include/player-interactions-api.h
COPY --from=build-sidecar /build/libsidecar-cpp/include/battleground-api.h ./repo/deps/libsidecar/include/battleground-api.h
COPY --from=build-sidecar /build/libsidecar-cpp/include/monitoring.h ./repo/deps/libsidecar/include/monitoring.h
COPY --from=build-sidecar /build/libsidecar-cpp/build/generated/include/tc9_version.h ./repo/deps/libsidecar/include/tc9_version.h

WORKDIR /repo

RUN --mount=type=cache,id=ptr-guards-clean-ac-build,target=/repo/build \
    rm -f /repo/build/CMakeCache.txt && \
    cmake -S /repo -B /repo/build -DUSE_REAL_LIBSIDECAR=ON \
        -DBoost_USE_STATIC_LIBS=ON \
        -DWITH_WARNINGS=1 \
        -DWITH_COREDEBUG=0 \
        -DUSE_COREPCH=1 \
        -DUSE_SCRIPTPCH=1 \
        -DTOOLS_BUILD=db-only \
        -DSCRIPTS=static \
        -DSERVERS=1 \
        -DCMAKE_BUILD_TYPE=RelWithDebInfo \
        -DCMAKE_INSTALL_PREFIX=/repo/install \
        -DBUILD_TESTING=0

RUN cp deps/libsidecar/libsidecar.so /usr/lib/libsidecar.so

WORKDIR /repo/build

ARG CORE_BUILD_TARGET=all
ARG BUILD_JOBS=4
RUN --mount=type=cache,id=ptr-guards-clean-ac-build,target=/repo/build \
    cmake --build . --config "RelWithDebInfo" --target "${CORE_BUILD_TARGET}" -j "${BUILD_JOBS}"
RUN --mount=type=cache,id=ptr-guards-clean-ac-build,target=/repo/build \
    if [ "${CORE_BUILD_TARGET}" = "all" ]; then cmake --install . --config "RelWithDebInfo"; fi

FROM ubuntu:22.04 AS db-import

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      libmysqlclient21 libreadline8 libgrpc++1 libprotobuf23 \
      gettext-base default-mysql-client && \
    rm -rf /var/lib/apt/lists/*


COPY --from=build-sidecar /build/libsidecar-cpp/build/libsidecar.so /usr/lib/libsidecar.so

# The paths are hardcoded in the binary
COPY --from=build-core /repo/data /repo/data
COPY --from=build-core /repo/install/bin/dbimport /repo/bin/dbimport
COPY --from=build-core /repo/src/tools/dbimport/dbimport.conf.dist /repo/install/etc/dbimport.conf

COPY sql/auth/mysql/ /repo/data/sql/custom/db_auth
COPY sql/characters/mysql/*.up.sql /repo/data/sql/custom/db_characters
COPY sql/world/mysql/ /repo/data/sql/custom/db_world
WORKDIR /repo/bin

ENTRYPOINT ["./dbimport"]

FROM ubuntu:22.04 AS gameserver

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      libmysqlclient21 libreadline8 libgrpc++1 libprotobuf23 \
      gettext-base default-mysql-client && \
    rm -rf /var/lib/apt/lists/*

COPY --from=build-sidecar /build/libsidecar-cpp/build/libsidecar.so /usr/lib/libsidecar.so

COPY --from=build-core /repo/install/etc/worldserver.conf.dist /repo/install/etc/worldserver.conf
COPY --from=build-core /repo/install/bin/worldserver /repo/bin/worldserver

WORKDIR /repo/bin

ENTRYPOINT ["./worldserver"]
